# Databricks notebook source
"""Datadog cost and usage extraction plus bronze staging."""

from __future__ import annotations

import json
import logging
import re
from dataclasses import dataclass
from datetime import UTC, date, datetime, timedelta
from typing import Any

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class DdBronzeConfig:
    catalog_name: str
    dd_site: str
    billing_month: str
    prior_billing_month: str
    run_date: date
    daily_cost_lag_days: int = 3
    hourly_usage_product_families: str = "all"


def _widget_value(name: str, default: str) -> str:
    try:
        return globals()["dbutils"].widgets.get(name)
    except Exception:
        return default


def _optional_widget_value(name: str) -> str | None:
    value = _widget_value(name, "").strip()
    return value or None


_PARAM_KEY_PATTERN = re.compile(r"[a-z_]+")


def _pipeline_parameter(spark_session: Any, catalog_name: str, key: str) -> str | None:
    if not _PARAM_KEY_PATTERN.fullmatch(key):
        raise ValueError(f"Invalid pipeline parameter key: {key}")
    try:
        rows = spark_session.sql(
            f"SELECT param_value FROM {catalog_name}.reference.pipeline_parameters WHERE param_key = '{key}' LIMIT 1"
        ).collect()
    except Exception:
        return None
    if not rows:
        return None
    return str(rows[0][0])


def _billing_month_from_run_date(run_date: str | None) -> str | None:
    if not run_date:
        return None
    return datetime.strptime(run_date, "%Y-%m-%d").strftime("%Y-%m")


def _date_from_run_date(run_date: str | None) -> date | None:
    if not run_date:
        return None
    return datetime.strptime(run_date, "%Y-%m-%d").date()


def build_config(spark_session: Any | None = None) -> DdBronzeConfig:
    catalog_name = _widget_value("catalog_name", "cost_analytics_dev")
    widget_run_date = _optional_widget_value("run_date")
    run_date_billing_month = _billing_month_from_run_date(widget_run_date)

    table_billing_month = _pipeline_parameter(spark_session, catalog_name, "dd_billing_month") if spark_session else None
    table_prior_month = _pipeline_parameter(spark_session, catalog_name, "dd_prior_billing_month") if spark_session else None
    table_run_date = _pipeline_parameter(spark_session, catalog_name, "dd_run_date") if spark_session else None
    table_lag_days = _pipeline_parameter(spark_session, catalog_name, "dd_daily_cost_lag_days") if spark_session else None

    billing_month = run_date_billing_month or table_billing_month or date.today().strftime("%Y-%m")
    prior_billing_month = table_prior_month or _prior_billing_month(billing_month)
    run_date = _date_from_run_date(widget_run_date or table_run_date) or date.today()
    daily_cost_lag_days = int(table_lag_days) if table_lag_days is not None else 3

    return DdBronzeConfig(
        catalog_name=catalog_name,
        dd_site=_widget_value("dd_site", "us3.datadoghq.com"),
        billing_month=billing_month,
        prior_billing_month=prior_billing_month,
        run_date=run_date,
        daily_cost_lag_days=daily_cost_lag_days,
    )


def _prior_billing_month(billing_month: str) -> str:
    billing_month_date = datetime.strptime(f"{billing_month}-01", "%Y-%m-%d")
    return (billing_month_date - timedelta(days=1)).strftime("%Y-%m")


def choose_cost_source(billing_period: str, today: date | None = None) -> str:
    today = today or date.today()
    bp_year, bp_month = (int(part) for part in billing_period.split("-"))
    if bp_year == today.year and bp_month == today.month:
        return "estimated"
    months_diff = (today.year - bp_year) * 12 + (today.month - bp_month)
    if months_diff == 1:
        return "historical" if today.day >= 16 else "estimated"
    return "historical"


def lag_adjusted_end_date(dt: date, lag_days: int = 3) -> date | None:
    adjusted = dt - timedelta(days=lag_days)
    first_of_month = dt.replace(day=1)
    if adjusted < first_of_month:
        return None
    return adjusted


def daily_cost_end_date(today: date, billing_month: str, lag_days: int = 3) -> date | None:
    first_of_month = datetime.strptime(f"{billing_month}-01", "%Y-%m-%d").date()
    lagged_today = today - timedelta(days=lag_days)
    if lagged_today < first_of_month:
        return None
    return min(lagged_today, _last_day_of_month(billing_month))


def _get_secrets() -> tuple[str, str]:
    try:
        api_key = globals()["dbutils"].secrets.get(scope="datadog", key="api_key")
        app_key = globals()["dbutils"].secrets.get(scope="datadog", key="app_key")
    except Exception as exc:
        raise RuntimeError("Failed to read Datadog secrets from scope 'datadog'") from exc
    return api_key, app_key


def _auth_headers(api_key: str, app_key: str) -> dict[str, str]:
    return {"DD-API-KEY": api_key, "DD-APPLICATION-KEY": app_key}


def _fetch_cost(dd_site: str, billing_period: str, source: str, headers: dict[str, str]) -> dict[str, Any]:
    import httpx

    if source == "estimated":
        url = f"https://api.{dd_site}/api/v2/usage/estimated_cost"
    else:
        url = f"https://api.{dd_site}/api/v2/usage/historical_cost"
    params = {"view": "sub-org", "start_month": billing_period}
    with httpx.Client(timeout=60) as client:
        response = client.get(url, params=params, headers=headers)
    response.raise_for_status()
    return response.json()


def _fetch_usage(dd_site: str, billing_period: str, headers: dict[str, str]) -> dict[str, Any]:
    import httpx

    url = f"https://api.{dd_site}/api/v1/usage/summary"
    params = {"start_month": billing_period, "end_month": billing_period}
    with httpx.Client(timeout=60) as client:
        response = client.get(url, params=params, headers=headers)
    response.raise_for_status()
    return response.json()


def _fetch_daily_cost(dd_site: str, billing_period: str, end_date: date, headers: dict[str, str]) -> dict[str, Any]:
    import httpx

    url = f"https://api.{dd_site}/api/v2/usage/estimated_cost"
    params = {"view": "sub-org", "start_date": f"{billing_period}-01", "end_date": end_date.isoformat()}
    with httpx.Client(timeout=60) as client:
        response = client.get(url, params=params, headers=headers)
    response.raise_for_status()
    return response.json()


_UNIT_COMPONENT_ORDER = ["top99p", "bytes", "agg_sum", "hwm", "avg", "sum"]
_UNIT_MAP = {
    "top99p": "hosts",
    "bytes": "bytes",
    "agg_sum": "events",
    "sum": "events",
    "avg": "instances",
    "hwm": "count",
}


def _derive_unit(metric_name: str) -> str:
    for component in _UNIT_COMPONENT_ORDER:
        if f"_{component}" in metric_name or metric_name.startswith(f"{component}_"):
            return _UNIT_MAP[component]
    return "unknown"


def _required_text(value: Any) -> str | None:
    if value is None:
        return None
    text = str(value).strip()
    return text or None


def _required_cost(value: Any) -> float | None:
    if value is None:
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def _aggregate_rows(rows: list[dict[str, Any]], key_fields: tuple[str, ...]) -> list[dict[str, Any]]:
    grouped: dict[tuple[Any, ...], dict[str, Any]] = {}
    raw_payloads: dict[tuple[Any, ...], list[Any]] = {}
    for row in rows:
        key = tuple(row[field] for field in key_fields)
        if key not in grouped:
            grouped[key] = {field: row[field] for field in key_fields}
            grouped[key]["cost_usd"] = 0.0
            if "data_freshness" in row:
                grouped[key]["data_freshness"] = row["data_freshness"]
            grouped[key]["_ingestion_timestamp"] = row["_ingestion_timestamp"]
            raw_payloads[key] = []
        grouped[key]["cost_usd"] += row["cost_usd"]
        if "_raw_payload" in row:
            raw_payloads[key].append(row["_raw_payload"])

    aggregated = list(grouped.values())
    for row in aggregated:
        key = tuple(row[field] for field in key_fields)
        row["cost_usd"] = round(row["cost_usd"], 10)
        if raw_payloads[key]:
            row["_raw_json"] = json.dumps(raw_payloads[key], sort_keys=True)
    return aggregated


def normalize_cost_items(cost_response: dict[str, Any], billing_period: str, data_freshness: str) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for entry in cost_response.get("data", []):
        for charge in entry.get("attributes", {}).get("charges", []):
            if charge.get("charge_type") == "total":
                continue
            product_name = _required_text(charge.get("product_name"))
            charge_type = _required_text(charge.get("charge_type"))
            cost_usd = _required_cost(charge.get("cost"))
            if not product_name or not charge_type or cost_usd is None:
                continue
            rows.append(
                {
                    "billing_period": billing_period,
                    "product_name": product_name,
                    "charge_type": charge_type,
                    "cost_usd": cost_usd,
                    "data_freshness": data_freshness,
                    "_raw_payload": charge,
                    "_ingestion_timestamp": datetime.now(UTC),
                }
            )
    return _aggregate_rows(rows, ("billing_period", "product_name", "charge_type"))


def normalize_daily_costs(daily_cost_response: dict[str, Any], billing_period: str) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for entry in daily_cost_response.get("data", []):
        attributes = entry.get("attributes", {})
        raw_date = _required_text(attributes.get("date"))
        if not raw_date:
            continue
        date_str = raw_date[:10]
        try:
            datetime.strptime(date_str, "%Y-%m-%d")
        except ValueError:
            continue
        for charge in attributes.get("charges", []):
            if charge.get("charge_type") == "total":
                continue
            product_name = _required_text(charge.get("product_name"))
            charge_type = _required_text(charge.get("charge_type"))
            cost_usd = _required_cost(charge.get("cost"))
            if not product_name or not charge_type or cost_usd is None:
                continue
            rows.append(
                {
                    "billing_period": billing_period,
                    "cost_date": date_str,
                    "product_name": product_name,
                    "charge_type": charge_type,
                    "cost_usd": cost_usd,
                    "_ingestion_timestamp": datetime.now(UTC),
                }
            )
    return _aggregate_rows(rows, ("billing_period", "cost_date", "product_name", "charge_type"))


def _usage_metric_items(usage_response: dict[str, Any]) -> list[tuple[str, Any]]:
    data = usage_response.get("data")
    if isinstance(data, dict):
        return list(data.items())
    usage_entries = usage_response.get("usage")
    if isinstance(usage_entries, list):
        items: list[tuple[str, Any]] = []
        for entry in usage_entries:
            if isinstance(entry, dict):
                items.extend(entry.items())
        return items
    return []


def normalize_usage_metrics(usage_response: dict[str, Any], billing_period: str) -> list[dict[str, Any]]:
    metric_values: dict[str, float] = {}
    for metric_name, value in _usage_metric_items(usage_response):
        if value is None:
            continue
        try:
            metric_values[metric_name] = metric_values.get(metric_name, 0.0) + float(value)
        except (TypeError, ValueError):
            continue

    rows: list[dict[str, Any]] = []
    for metric_name, value in metric_values.items():
        rows.append(
            {
                "billing_period": billing_period,
                "metric_name": metric_name,
                "metric_value": value,
                "unit": _derive_unit(metric_name),
                "_ingestion_timestamp": datetime.now(UTC),
            }
        )
    return rows


def create_cost_items_table_sql(catalog_name: str) -> str:
    return f"""
    CREATE TABLE IF NOT EXISTS {catalog_name}.dd_bronze.cost_items (
      billing_period STRING,
      product_name STRING,
      charge_type STRING,
      cost_usd DOUBLE,
      data_freshness STRING,
      _raw_json STRING,
      _ingestion_timestamp TIMESTAMP
    ) USING DELTA
    CLUSTER BY (billing_period, product_name)
    TBLPROPERTIES (
      'delta.autoOptimize.optimizeWrite' = 'true',
      'delta.autoOptimize.autoCompact' = 'true',
      'delta.enableChangeDataFeed' = 'true'
    )
    """


def create_daily_costs_table_sql(catalog_name: str) -> str:
    return f"""
    CREATE TABLE IF NOT EXISTS {catalog_name}.dd_bronze.daily_costs (
      billing_period STRING,
      cost_date STRING,
      product_name STRING,
      charge_type STRING,
      cost_usd DOUBLE,
      _ingestion_timestamp TIMESTAMP
    ) USING DELTA
    CLUSTER BY (billing_period, cost_date)
    TBLPROPERTIES (
      'delta.autoOptimize.optimizeWrite' = 'true',
      'delta.autoOptimize.autoCompact' = 'true',
      'delta.enableChangeDataFeed' = 'true'
    )
    """


def create_usage_metrics_table_sql(catalog_name: str) -> str:
    return f"""
    CREATE TABLE IF NOT EXISTS {catalog_name}.dd_bronze.usage_metrics (
      billing_period STRING,
      metric_name STRING,
      metric_value DOUBLE,
      unit STRING,
      _ingestion_timestamp TIMESTAMP
    ) USING DELTA
    CLUSTER BY (billing_period, metric_name)
    TBLPROPERTIES (
      'delta.autoOptimize.optimizeWrite' = 'true',
      'delta.autoOptimize.autoCompact' = 'true',
      'delta.enableChangeDataFeed' = 'true'
    )
    """


def merge_cost_items_sql(catalog_name: str) -> str:
    return f"""
    MERGE INTO {catalog_name}.dd_bronze.cost_items target
    USING dd_cost_items_stage source
    ON target.billing_period = source.billing_period
      AND target.product_name = source.product_name
      AND target.charge_type = source.charge_type
    WHEN MATCHED THEN UPDATE SET *
    WHEN NOT MATCHED THEN INSERT *
    """


def merge_daily_costs_sql(catalog_name: str) -> str:
    return f"""
    MERGE INTO {catalog_name}.dd_bronze.daily_costs target
    USING dd_daily_costs_stage source
    ON target.billing_period = source.billing_period
      AND target.cost_date = source.cost_date
      AND target.product_name = source.product_name
      AND target.charge_type = source.charge_type
    WHEN MATCHED THEN UPDATE SET *
    WHEN NOT MATCHED THEN INSERT *
    """


def merge_usage_metrics_sql(catalog_name: str) -> str:
    return f"""
    MERGE INTO {catalog_name}.dd_bronze.usage_metrics target
    USING dd_usage_metrics_stage source
    ON target.billing_period = source.billing_period
      AND target.metric_name = source.metric_name
    WHEN MATCHED THEN UPDATE SET *
    WHEN NOT MATCHED THEN INSERT *
    """


def _last_day_of_month(billing_month: str) -> date:
    bp_year, bp_month = (int(part) for part in billing_month.split("-"))
    if bp_month == 12:
        return date(bp_year + 1, 1, 1) - timedelta(days=1)
    return date(bp_year, bp_month + 1, 1) - timedelta(days=1)


def run(spark_session: Any, config: DdBronzeConfig) -> dict[str, Any]:
    api_key, app_key = _get_secrets()
    headers = _auth_headers(api_key, app_key)

    cost_source = choose_cost_source(config.billing_month, config.run_date)
    data_freshness = "finalized" if cost_source == "historical" else "estimated"
    cost_rows = normalize_cost_items(
        _fetch_cost(config.dd_site, config.billing_month, cost_source, headers),
        config.billing_month,
        data_freshness,
    )

    usage_rows = normalize_usage_metrics(_fetch_usage(config.dd_site, config.billing_month, headers), config.billing_month)
    if not usage_rows:
        logger.warning("Datadog usage API returned no metrics for %s", config.billing_month)

    daily_rows: list[dict[str, Any]] = []
    daily_cost_status = "skipped"
    lag_end = daily_cost_end_date(config.run_date, config.billing_month, config.daily_cost_lag_days)
    if lag_end:
        try:
            daily_rows = normalize_daily_costs(
                _fetch_daily_cost(config.dd_site, config.billing_month, lag_end, headers),
                config.billing_month,
            )
            daily_cost_status = "success"
        except Exception as exc:
            logger.warning("Daily cost fetch failed (non-fatal): %s", exc)
            daily_cost_status = "failed"

    spark_session.sql(f"CREATE SCHEMA IF NOT EXISTS {config.catalog_name}.dd_bronze")
    spark_session.sql(create_cost_items_table_sql(config.catalog_name))
    spark_session.sql(create_daily_costs_table_sql(config.catalog_name))
    spark_session.sql(create_usage_metrics_table_sql(config.catalog_name))

    counts: dict[str, Any] = {"daily_cost_status": daily_cost_status}
    if not cost_rows:
        raise RuntimeError("Datadog cost API returned no charge items")

    spark_session.createDataFrame(cost_rows).createOrReplaceTempView("dd_cost_items_stage")
    spark_session.sql(merge_cost_items_sql(config.catalog_name))
    counts["cost_items"] = len(cost_rows)

    if usage_rows:
        spark_session.createDataFrame(usage_rows).createOrReplaceTempView("dd_usage_metrics_stage")
        spark_session.sql(merge_usage_metrics_sql(config.catalog_name))
    counts["usage_metrics"] = len(usage_rows)

    if daily_rows:
        spark_session.createDataFrame(daily_rows).createOrReplaceTempView("dd_daily_costs_stage")
        spark_session.sql(merge_daily_costs_sql(config.catalog_name))
    counts["daily_costs"] = len(daily_rows)

    return counts


def main() -> None:
    try:
        spark_session = spark  # type: ignore[name-defined]
    except NameError as exc:
        raise RuntimeError("Spark session is required for Datadog bronze staging") from exc
    counts = run(spark_session, build_config(spark_session))
    for key, value in counts.items():
        print(f"dd bronze {key}: {value}")


if __name__ == "__main__":
    main()
