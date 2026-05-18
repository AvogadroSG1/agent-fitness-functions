# CALM PoC Threshold Exceptions

Generated from baseline reports on 2026-05-18. These existing results exceed or fall below the proposed shared governance thresholds and require HITL treatment as baseline exceptions unless a future change worsens them.

## Proposed Thresholds

| Fitness Function | Operator | Threshold |
| --- | --- | ---: |
| Cyclomatic Complexity | `lte` | 9 |
| Interface Width | `lte` | 20 |
| Implementation Depth | `gte` | 0.722 |
| Logic Density | `gte` | 0.255 |
| Dependency Discipline | `gte` | 0 |

## Exception Counts

| Repository | Language | CC > 9 | Public Methods > 20 | Avg LOC/Public < 0.722 | LDR < 0.255 | DDC < 0 |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| graft | go | 49 | 2 | 0 | 0 | 0 |
| ringstation | python | 137 | 36 | 0 | 18 | 0 |
| SlackStatus | csharp | 2 | 1 | 6 | 6 | 0 |
| StackOverflow.Api.V3 | csharp | 23 | 31 | 7 | 31 | 0 |

## graft (go)

### Cyclomatic Complexity

| File | Function | CC |
| --- | --- | ---: |
| `/Users/poconnor/peter_code/graft/internal/migrate/migrate.go` | `Chain` | 24 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `newMCPAddCommand` | 18 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `newInitCommandWithDeps` | 18 |
| `/Users/poconnor/peter_code/graft/internal/claudecfg/claudecfg_test.go` | `TestLoadGroupsGlobalLocalAndProjectStdioMCPs` | 18 |
| `/Users/poconnor/peter_code/graft/internal/claudecfg/claudecfg_test.go` | `TestLoadHTTPRoundTripRendersClaudeAndCodex` | 17 |
| `/Users/poconnor/peter_code/graft/internal/sync/sync.go` | `ApplyWithOptions` | 16 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `newSyncCommandWithDeps` | 15 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `newLibraryMigrateFromClaudeCommand` | 15 |
| `/Users/poconnor/peter_code/graft/cmd/migrate_from_claude_test.go` | `TestLibraryMigrateFromClaudeCreatesLibraryAndRegistersIt` | 15 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `newMCPImportCommand` | 14 |
| `/Users/poconnor/peter_code/graft/internal/render/render_test.go` | `TestAdaptersRenderHTTPTransportWithoutStdioFields` | 14 |
| `/Users/poconnor/peter_code/graft/internal/status/status.go` | `renderedConfigWithDetail` | 13 |
| `/Users/poconnor/peter_code/graft/internal/sync/sync.go` | `renderTarget` | 13 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `newPickCommandWithDeps` | 13 |
| `/Users/poconnor/peter_code/graft/cmd/pick_command_test.go` | `TestPickCommandWritesConfirmedSelectionToLock` | 13 |
| `/Users/poconnor/peter_code/graft/internal/claudecfg/claudecfg_test.go` | `TestLoadParsesRemoteMCPsAtAllScopes` | 13 |
| `/Users/poconnor/peter_code/graft/cmd/root_test.go` | `TestInitCommandLaunchesPickForSelectedLibrary` | 13 |
| `/Users/poconnor/peter_code/graft/internal/render/render_test.go` | `TestCodexAdapterPreservesUnrelatedSettings` | 13 |
| `/Users/poconnor/peter_code/graft/internal/status/status.go` | `ResolveWithDefinitions` | 13 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `savePickResultWithSideEffects` | 12 |
| `/Users/poconnor/peter_code/graft/internal/sync/sync.go` | `migrateDefinition` | 12 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `ensureLockLibrariesRegistered` | 12 |
| `/Users/poconnor/peter_code/graft/internal/sync/sync.go` | `enforcePinForTargets` | 12 |
| `/Users/poconnor/peter_code/graft/internal/tui/pick.go` | `Update` | 12 |
| `/Users/poconnor/peter_code/graft/cmd/root_test.go` | `TestStatusCommandPromptsToRegisterUnknownLockLibrary` | 12 |
| `/Users/poconnor/peter_code/graft/cmd/mcp_authoring_test.go` | `TestMCPPushYesCommitsAndPushes` | 12 |
| `/Users/poconnor/peter_code/graft/cmd/library_command_test.go` | `TestLibraryAddRegistersClonesAndSetsFirstAsDefault` | 12 |
| `/Users/poconnor/peter_code/graft/internal/library/library_test.go` | `TestImportFileParsesHTTPTransports` | 12 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `newStatusCommandWithDeps` | 11 |
| `/Users/poconnor/peter_code/graft/internal/pin/pin.go` | `firstPackageArg` | 11 |
| `/Users/poconnor/peter_code/graft/cmd/sync_command_test.go` | `TestSyncCommandRegistersUnknownLockLibraryBeforePull` | 11 |
| `/Users/poconnor/peter_code/graft/cmd/pick_command_test.go` | `TestPickCommandRemovesUncheckedMCPs` | 11 |
| `/Users/poconnor/peter_code/graft/cmd/mcp_authoring_test.go` | `TestMCPAddPromptsForFields` | 11 |
| `/Users/poconnor/peter_code/graft/cmd/migrate_from_claude_test.go` | `TestLibraryMigrateFromClaudeIncludesRemoteMCPsInApprovalFlow` | 11 |
| `/Users/poconnor/peter_code/graft/cmd/pick_helpers_test.go` | `TestApplyPickResultWritesSelectedMCPsAndEmptySlice` | 11 |
| `/Users/poconnor/peter_code/graft/internal/migrate/migrate.go` | `ApplyWithInput` | 11 |
| `/Users/poconnor/peter_code/graft/internal/model/model.go` | `Adapter` | 11 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `writeImportedDefinition` | 10 |
| `/Users/poconnor/peter_code/graft/internal/render/render.go` | `writeCodex` | 10 |
| `/Users/poconnor/peter_code/graft/internal/render/render.go` | `writeClaude` | 10 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `statusRowState` | 10 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `promptApproval` | 10 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `prepareLocalLibraryConfig` | 10 |
| `/Users/poconnor/peter_code/graft/internal/status/status.go` | `pinMismatchDetail` | 10 |
| `/Users/poconnor/peter_code/graft/cmd/root.go` | `newLibraryShowCommand` | 10 |
| `/Users/poconnor/peter_code/graft/internal/sync/sync.go` | `credentialMapHasSensitiveFields` | 10 |
| `/Users/poconnor/peter_code/graft/internal/library/library.go` | `WriteDefinitionFile` | 10 |
| `/Users/poconnor/peter_code/graft/internal/render/render_test.go` | `TestAdaptersRefuseUnmanagedOverwriteAndPreserveOnRemove` | 10 |
| `/Users/poconnor/peter_code/graft/internal/claudecfg/claudecfg.go` | `Load` | 10 |

### Interface Width

| File | Public Methods |
| --- | ---: |
| `/Users/poconnor/peter_code/graft/internal/sync/sync_test.go` | 33 |
| `/Users/poconnor/peter_code/graft/cmd/root_test.go` | 22 |

### Implementation Depth

No exceptions.

### Logic Density

No exceptions.

### Dependency Discipline

No exceptions.

## ringstation (python)

### Cyclomatic Complexity

| File | Function | CC |
| --- | --- | ---: |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/projections/daily_digest.py` | `project` | 36 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/azure_costs_gold_customer.py` | `transform` | 32 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/transforms/azure_costs_gold_customer.py` | `transform` | 32 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/transforms/azure_costs_gold_trends.py` | `transform` | 31 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/adapters/driving/cli.py` | `catchup` | 30 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station/src/ring_station/adapters/driving/cli.py` | `catchup` | 30 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/runners.py` | `_run_gcp_costs` | 29 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/calendar_silver.py` | `_classify_event` | 28 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/calendar_silver.py` | `_classify_event` | 28 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/azure_costs_gold_trends.py` | `transform` | 27 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/src/setup/validate_configs.py` | `load_dd_costs_config` | 27 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/azure_costs_silver_customer.py` | `transform` | 26 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/azure_costs_gold_customer_resources.py` | `transform` | 26 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/transforms/azure_costs_silver_customer.py` | `transform` | 26 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/transforms/azure_costs_gold_customer_resources.py` | `transform` | 26 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/azure_costs_gold_service_category.py` | `transform` | 25 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/transforms/azure_costs_gold_service_category.py` | `transform` | 25 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/gemini_notes_silver.py` | `_parse_action_items` | 24 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/gemini_notes_silver.py` | `_parse_action_items` | 24 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_gcp_task_1_catalog_reference_staging_and_silver_assets` | 23 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_bamboohr_gold.py` | `test_frontmatter_contains_all_bamboohr_fields` | 23 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/bamboohr/test_bamboohr_gold.py` | `test_frontmatter_contains_all_bamboohr_fields` | 23 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_dd_task_10_reconciliation_contract_is_encoded` | 20 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_dd_task_9_usage_efficiency_contract_is_encoded` | 19 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_dd_task_8_daily_gold_contract_is_encoded` | 19 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/application/pipeline.py` | `_run_drive_transforms` | 19 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/runners.py` | `_run_drive` | 19 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/gemini_notes_bronze.py` | `_parse_transcript_turns` | 19 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/gemini_notes_bronze.py` | `_parse_transcript_turns` | 19 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/adapters/driving/deps.py` | `build_pipeline_order` | 18 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station/src/ring_station/adapters/driving/deps.py` | `build_pipeline_order` | 18 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/application/pipeline.py` | `_run_gcp_costs_transforms` | 17 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/gcp_costs_gold_service_category.py` | `transform` | 16 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/transforms/gcp_costs_gold_service_category.py` | `transform` | 16 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_gcp_task_3_drilldown_assets_and_validation_hooks_exist` | 16 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/azure_costs_gold_service_resources.py` | `transform` | 15 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/transforms/azure_costs_gold_service_resources.py` | `transform` | 15 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_reference_config_templates_match_silver_contract_and_are_sanitized` | 15 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/projections/feedback_loop.py` | `project` | 15 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/src/validation/post_refresh_check.py` | `assert_dd_gold_non_empty_when_bronze_has_data` | 15 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/gmail_silver.py` | `_classify_message` | 15 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/gmail_silver.py` | `_classify_message` | 15 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/transforms/gcp_costs_gold_internal.py` | `transform` | 14 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure-billing-export/src/ring_station_azure_billing_export/transforms/billing_export_silver_customer.py` | `transform` | 14 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_bamboohr_bronze.py` | `test_compensation_fields_populated` | 14 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/bamboohr/test_bamboohr_bronze.py` | `test_compensation_fields_populated` | 14 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/projections/open_questions.py` | `project` | 14 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-bamboohr/src/ring_station_bamboohr/runners.py` | `_run_bamboohr` | 14 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/calendar_silver.py` | `_detect_multi_booking` | 14 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/calendar_silver.py` | `_detect_multi_booking` | 14 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/azure_costs_gold_internal.py` | `transform` | 13 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/transforms/azure_costs_gold_internal.py` | `transform` | 13 |
| `/Users/poconnor/peter_code/ringstation/tests/integration/test_azure_costs_customer_pipeline.py` | `test_full_pipeline_produces_gold_keys` | 13 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_azure_costs_gold_customer_resources.py` | `test_basic_shape` | 13 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/azure/test_azure_costs_gold_customer_resources.py` | `test_basic_shape` | 13 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/adapters/driving/cli.py` | `show_schedule` | 13 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station/src/ring_station/adapters/driving/cli.py` | `show_schedule` | 13 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/arm_parser.py` | `parse_arm_resource_id` | 13 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/transforms/arm_parser.py` | `parse_arm_resource_id` | 13 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/application/pipeline.py` | `_run_bamboohr_transforms` | 13 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/application/pipeline.py` | `_run_azure_costs_internal_transforms` | 13 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/application/pipeline.py` | `_run_azure_costs_customer_transforms` | 13 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/gemini_notes_bronze.py` | `_parse_notes_section` | 13 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/gemini_notes_bronze.py` | `_parse_notes_section` | 13 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/gmail_silver.py` | `__init__` | 13 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/gmail_silver.py` | `__init__` | 13 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/slack_silver.py` | `transform` | 12 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/gcp_costs_gold_community.py` | `transform` | 12 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-slack/src/ring_station_slack/transforms/slack_silver.py` | `transform` | 12 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/transforms/gcp_costs_gold_shared.py` | `transform` | 12 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/transforms/gcp_costs_gold_community.py` | `transform` | 12 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-datadog/src/ring_station_datadog/transforms/datadog_costs_gold_by_category.py` | `transform` | 12 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_azure_costs_silver_customer.py` | `test_silver_resource_rows_customer_attribution` | 12 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/azure/test_azure_costs_silver_customer.py` | `test_silver_resource_rows_customer_attribution` | 12 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_gcp_task_2_gold_mom_anomaly_and_allocation_semantics` | 12 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/application/pipeline.py` | `_run_transforms` | 12 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure-billing-export/src/ring_station_azure_billing_export/transforms/billing_export_bronze.py` | `_parse_export` | 12 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/slack_silver.py` | `_group_threads` | 12 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-slack/src/ring_station_slack/transforms/slack_silver.py` | `_group_threads` | 12 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/slack_silver.py` | `_extract_entities` | 12 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-slack/src/ring_station_slack/transforms/slack_silver.py` | `_extract_entities` | 12 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/gcp_costs_gold_service_resources.py` | `transform` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/transforms/gcp_costs_gold_marketplace.py` | `transform` | 11 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_gcp_task_2_summary_gold_assets_exist_and_follow_contract` | 11 |
| `/Users/poconnor/peter_code/ringstation/tests/integration/test_slack_gold_pipeline.py` | `test_full_pipeline_produces_thread_records` | 11 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/azure-billing-export/test_billing_export_bronze.py` | `test_column_mapping` | 11 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_calendar_bronze.py` | `test_basic_field_mapping` | 11 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/google/test_calendar_bronze.py` | `test_basic_field_mapping` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/transforms/project_classifier.py` | `split_line_items` | 11 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/projections/missed_meetings.py` | `project` | 11 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/src/setup/dd_stage_bronze.py` | `build_config` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/runners.py` | `_run_internal` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure-billing-export/src/ring_station_azure_billing_export/runners.py` | `_run_gold` | 11 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/application/pipeline.py` | `_run_gemini_notes_transforms` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/runners.py` | `_run_customer` | 11 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/drive_gold.py` | `_extract_unpushed_callouts` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/drive_gold.py` | `_extract_unpushed_callouts` | 11 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/transforms/calendar_silver.py` | `_detect_changes` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/transforms/calendar_silver.py` | `_detect_changes` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-slack/src/ring_station_slack/transforms/slack_silver.py` | `__init__` | 11 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/transforms/gcp_costs_gold_service_resources.py` | `transform` | 10 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-datadog/src/ring_station_datadog/transforms/datadog_costs_silver.py` | `transform` | 10 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-datadog/src/ring_station_datadog/transforms/datadog_costs_gold_reconciliation.py` | `transform` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_azure_costs_gold_customer_resources.py` | `test_unattributed_resources` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/azure/test_azure_costs_gold_customer_resources.py` | `test_unattributed_resources` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_task_2_review_resolutions_are_encoded` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_silver_uses_spec_dedup_and_customer_attribution` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/azure-billing-export/test_billing_export_runner.py` | `test_gold_writes_to_virtual_source_names_with_write_by_key` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/datadog/test_datadog_runner.py` | `test_gold_writes_four_keys_and_publishes` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_gcp_task_4_bundle_dag_dependencies_are_structural` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_gcp_task_3_review_resolutions_are_encoded` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_gcp_task_2_reconciliation_accounts_for_all_lanes` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_gcp_task_1_config_templates_are_sanitized_and_validatable` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_backfill_idempotency.py` | `test_dd_pipeline_parameters_are_derived_from_config_for_backfills` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_gcp_costs_gold_service_resources.py` | `test_category_service_resource_hierarchy` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/gcp/test_gcp_costs_gold_service_resources.py` | `test_category_service_resource_hierarchy` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | `test_bundle_has_two_pipelines_and_daily_job` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/datadog/test_datadog_runner.py` | `test_bronze_reads_writes_publishes` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_drive_bronze.py` | `test_bronze_normalizes_comment` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/google/test_drive_bronze.py` | `test_bronze_normalizes_comment` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_slack_bronze.py` | `test_bronze_message_has_required_fields` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/slack/test_slack_bronze.py` | `test_bronze_message_has_required_fields` | 10 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_gold_schema_contract.py` | `test_all_thread_fields_are_correct_types` | 10 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/domain/projections/leadership_voice.py` | `project` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/src/setup/validate_configs.py` | `load_subscription_mapping` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/src/setup/validate_configs.py` | `load_gcp_service_category_mapping` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/src/setup/validate_configs.py` | `load_dd_service_category_mapping` | 10 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/adapters/driving/cli.py` | `config_check` | 10 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station/src/ring_station/adapters/driving/cli.py` | `config_check` | 10 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/runners.py` | `_run_gemini_notes` | 10 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-datadog/src/ring_station_datadog/runners.py` | `_run_datadog_costs` | 10 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/adapters/driven/google_drive.py` | `_fetch_changes` | 10 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/adapters/google_drive.py` | `_fetch_changes` | 10 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/adapters/driven/slack.py` | `_build_user_cache` | 10 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-slack/src/ring_station_slack/adapters/slack.py` | `_build_user_cache` | 10 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/application/pipeline.py` | `_build_and_write_trends` | 10 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/src/setup/dd_stage_bronze.py` | `_aggregate_rows` | 10 |

### Interface Width

| File | Public Methods |
| --- | ---: |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_dashboard_components.py` | 82 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_slack_silver.py` | 76 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/slack/test_slack_silver.py` | 76 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_azure_dashboard_renderer.py` | 45 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/azure/test_azure_costs_gold_trends.py` | 41 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_azure_costs_gold_trends.py` | 36 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_calendar_silver.py` | 34 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/google/test_calendar_silver.py` | 34 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/datadog/test_datadog_costs_gold_by_category.py` | 34 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_expectations.py` | 32 |
| `/Users/poconnor/peter_code/ringstation/tests/integration/test_dashboard_browser.py` | 31 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/tests/test_dd_stage_bronze.py` | 31 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_gemini_notes_silver.py` | 28 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/google/test_gemini_notes_silver.py` | 28 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_gcp_costs_gold_community.py` | 27 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/gcp/test_gcp_costs_gold_community.py` | 27 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/datadog/test_datadog_costs_gold.py` | 27 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/datadog/test_datadog_costs_bronze.py` | 27 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_http_errors.py` | 26 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_gemini_notes_bronze.py` | 26 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/google/test_gemini_notes_bronze.py` | 26 |
| `/Users/poconnor/peter_code/ringstation/tests/core/test_http_errors.py` | 26 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_gcp_costs_gold_service_category.py` | 25 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/gcp/test_gcp_costs_gold_service_category.py` | 25 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/datadog/test_datadog_extractor.py` | 25 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/datadog/test_datadog_costs_gold_reconciliation.py` | 25 |
| `/Users/poconnor/peter_code/ringstation/databricks/cost-analytics/src/validation/post_refresh_check.py` | 24 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_gcp_costs_gold_service_resources.py` | 23 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/gcp/test_gcp_costs_gold_service_resources.py` | 23 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_cli_parity.py` | 22 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_azure_costs_gold_service_category.py` | 22 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/gcp/test_gcp_costs_gold_community_resources.py` | 22 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/azure/test_azure_costs_gold_service_category.py` | 22 |
| `/Users/poconnor/peter_code/ringstation/tests/core/test_cli_parity.py` | 22 |
| `/Users/poconnor/peter_code/ringstation/tests/unit/test_slack_gold.py` | 21 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/slack/test_slack_gold.py` | 21 |

### Implementation Depth

No exceptions.

### Logic Density

| File | LDR |
| --- | ---: |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/dashboards/layout.py` | 0.038 |
| `/Users/poconnor/peter_code/ringstation/tests/plugins/datadog/conftest.py` | 0.041 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/dashboards/components/trend_chart.py` | 0.045 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/dashboards/state.py` | 0.045 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/dashboards/components/resource_table.py` | 0.062 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/dashboards/components/sparkline.py` | 0.081 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/dashboards/components/bar_chart.py` | 0.092 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/dashboards/azure_costs_renderer.py` | 0.113 |
| `/Users/poconnor/peter_code/ringstation/tests/conftest.py` | 0.115 |
| `/Users/poconnor/peter_code/ringstation/tests/integration/conftest.py` | 0.119 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure/src/ring_station_azure/__init__.py` | 0.132 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-bamboohr/src/ring_station_bamboohr/__init__.py` | 0.189 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-google/src/ring_station_google/__init__.py` | 0.189 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-datadog/src/ring_station_datadog/__init__.py` | 0.196 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-gcp/src/ring_station_gcp/__init__.py` | 0.200 |
| `/Users/poconnor/peter_code/ringstation/src/ring_station/dashboards/components/breadcrumb.py` | 0.208 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-slack/src/ring_station_slack/__init__.py` | 0.209 |
| `/Users/poconnor/peter_code/ringstation/packages/ring-station-azure-billing-export/src/ring_station_azure_billing_export/__init__.py` | 0.214 |

### Dependency Discipline

No exceptions.

## SlackStatus (csharp)

### Cyclomatic Complexity

| File | Function | CC |
| --- | --- | ---: |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Slack/SlackChannelClient.cs` | `FetchAllChannelsAsync` | 12 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Web/Services/SlackStatusMcpTools.cs` | `TryBuildSchedule` | 10 |

### Interface Width

| File | Public Methods |
| --- | ---: |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Web/Models/BroadcastSettings.cs` | 36 |

### Implementation Depth

| File | Avg LOC/Public |
| --- | ---: |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Web/Configuration/SlackStatusSettings.cs` | 0.600 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Domain/Models/SlackUserStatus.cs` | 0.625 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Domain/Models/StatusConfig.cs` | 0.650 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Domain/Models/SystemState.cs` | 0.667 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Detectors/MeetingDetectionConfig.cs` | 0.667 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Detectors/WorkHoursConfig.cs` | 0.667 |

### Logic Density

| File | LDR |
| --- | ---: |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Slack/ISlackProfileClient.cs` | 0.200 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Detectors/IMusicDetector.cs` | 0.222 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Process/IProcessRunner.cs` | 0.222 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Slack/ISlackChatClient.cs` | 0.222 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Slack/ISlackEmojiClient.cs` | 0.222 |
| `/Users/poconnor/peter_code/SlackStatus/src/SlackStatus.Infrastructure/Slack/SlackChannel.cs` | 0.250 |

### Dependency Discipline

No exceptions.

## StackOverflow.Api.V3 (csharp)

### Cyclomatic Complexity

| File | Function | CC |
| --- | --- | ---: |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Flags/FlagService.cs` | `AddFlagToPostAsync` | 28 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Questions/QuestionService.cs` | `GetAllWithCursorAsync` | 24 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Auth/Authentication/IdentityBuilder.cs` | `Build` | 17 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Comments/CommentService.cs` | `EditAsync` | 14 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Comments/CommentService.cs` | `UpvoteAsync` | 13 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Attributes/AppTypeRequiredAttribute.cs` | `IsRequestAllowedForAppType` | 13 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Comments/CommentService.cs` | `GetAsync` | 13 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Comments/CommentService.cs` | `DeleteUpvoteAsync` | 13 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Comments/CommentService.cs` | `DeleteAsync` | 13 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Middleware/RateLimiter/RateLimitMiddleware.cs` | `InvokeAsync` | 12 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Middleware/ChannelProxyMiddleware.cs` | `InvokeAsync` | 12 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Auth/AccessTokenService.cs` | `GetValid` | 12 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Comments/CommentService.cs` | `CreateAsync` | 12 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Answers/AnswerService.cs` | `AcceptAsync` | 12 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/DiscussionReplies/DiscussionReplyRepository.cs` | `ToAddDiscussionReplyFlagResult` | 11 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Discussions/DiscussionRepository.cs` | `ToAddDiscussionFlagResult` | 11 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Comments/CommentService.cs` | `GetAllForPostAsync` | 11 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Questions/QuestionService.cs` | `GetAllAsync` | 11 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Answers/AnswerService.cs` | `EditAsync` | 11 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Swagger/AppTypeDocumentFilter.cs` | `Apply` | 11 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Answers/AnswerService.cs` | `UnacceptAsync` | 10 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/UrlHelper.cs` | `TryGetSiteAndTeamFromPath` | 10 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Middleware/TrafficLogResponseHeadersMiddleware.cs` | `InvokeAsync` | 10 |

### Interface Width

| File | Public Methods |
| --- | ---: |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/StaticQuarantine/EmptyDbContext.cs` | 137 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/QuestionResponseModel.cs` | 69 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/ArticlesResponseModel.cs` | 58 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/UnifiedSearchResultModel.cs` | 55 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/AnswerResponseModel.cs` | 54 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Tags/TagRepository.cs` | 53 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Posts/PostRepository.cs` | 45 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/UserResponseModel.cs` | 40 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Settings.cs` | 36 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Questions/QuestionService.cs` | 33 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/ProductEvents/HttpMessageProductEvent.cs` | 33 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/RevisionResponseModel.cs` | 33 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Tags/TagService.cs` | 31 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/UserGroups/UserGroupService.cs` | 27 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/IngestionSourceMetadataResponseModel.cs` | 26 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Users/CurrentUserPermissions.cs` | 25 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/SearchResultModel.cs` | 25 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Users/UserService.cs` | 24 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/TagBulkCreateResponseModel.cs` | 24 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/CommunityResponseModel.cs` | 24 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/PaginationModel.cs` | 24 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Questions/QuestionRepository.cs` | 23 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/CommentResponseModel.cs` | 23 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/UserGroups/UserGroupRepository.cs` | 22 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Accounts/AccountRepository.cs` | 22 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/TagResponseModel.cs` | 21 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/DiscussionResponseModel.cs` | 21 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/CollectionsResponseModel.cs` | 21 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Comments/CommentRepository.cs` | 21 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Articles/ArticleService.cs` | 21 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Answers/AnswerService.cs` | 21 |

### Implementation Depth

| File | Avg LOC/Public |
| --- | ---: |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/ArticleFilterRequestModel.cs` | 0.500 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/CommentCreateRequestModel.cs` | 0.500 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/CommentRequestModel.cs` | 0.500 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/CursorQuestionFilterRequestModel.cs` | 0.500 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/ManageUsersFilterRequestModel.cs` | 0.500 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/AuthMeResponseModel.cs` | 0.500 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/QuestionFilterRequestModel.cs` | 0.625 |

### Logic Density

| File | LDR |
| --- | ---: |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/UrlHelperFactory.cs` | 0.067 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Attributes/AllowCommonStaticAccessAttribute.cs` | 0.077 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Attributes/SwaggerHideForOverflowApiAttribute.cs` | 0.083 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/StaticQuarantine/ImmediateCacheWrapper.cs` | 0.091 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/CommentCreateRequestModel.cs` | 0.125 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Attributes/SwaggerOrderAttribute.cs` | 0.136 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Middleware/RateLimiter/IRateLimiter.cs` | 0.138 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Sites/SiteRepository.cs` | 0.179 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Attributes/SwaggerIgnoreInEnvironmentAttribute.cs` | 0.182 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Middleware/RateLimiter/NullRateLimiter.cs` | 0.182 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/AuthMeResponseModel.cs` | 0.182 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Revisions/RevisionSortParameter.cs` | 0.182 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Auth/Authentication/WorkloadIdentity/GcpWorkloadIdentityConfiguration.cs` | 0.200 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/SubjectMatterExpertRequestModel.cs` | 0.200 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Search/SearchRepository.cs` | 0.200 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/TagWatchersResponseModel.cs` | 0.214 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/ContentFeeds/ContentFeedChangeResults.cs` | 0.231 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/DiscussionReplies/DiscussionReplyResults.cs` | 0.231 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/IngestionFileUploadResponseModel.cs` | 0.231 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/ModuleIntegration/EventsModuleSettingsProvider.cs` | 0.231 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Swagger/ChannelsDisabledDocumentFilter.cs` | 0.240 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Helpers/HttpHelper.cs` | 0.241 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Swagger/SwaggerLimitSchemaFilter.cs` | 0.241 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Swagger/SwaggerIgnoreInEnvironmentSchemaFilter.cs` | 0.244 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Auth/Authentication/WorkloadIdentity/AzureWorkloadIdentityConfiguration.cs` | 0.250 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/CommunityJoinModel.cs` | 0.250 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Request/CommunityLeaveModel.cs` | 0.250 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/CollectionContentSummaryResponseModel.cs` | 0.250 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/Models/Response/IContainsPII.cs` | 0.250 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/ModuleIntegration/ModuleCurrentProvider.cs` | 0.250 |
| `/Users/poconnor/code/StackOverflow/StackOverflow.Api.V3/StaticQuarantine/ImmediateSiteCacheWrapper.cs` | 0.250 |

### Dependency Discipline

No exceptions.

