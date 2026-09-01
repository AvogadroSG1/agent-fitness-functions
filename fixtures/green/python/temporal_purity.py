"""Temporal purity green fixture: timezone-aware datetime construction."""

from datetime import datetime, timezone


def stamp_event(event):
    created = datetime.now(timezone.utc)
    event["created"] = created
    return event


def legacy_stamp(event):
    event["updated"] = datetime.now(tz=timezone.utc)
    return event
