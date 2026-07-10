# AlRouter July 2026 Performance Benchmark HTML Design

## Objective

Create a self-contained Korean HTML benchmark report for AlRouter model performance from `2026-07-01 00:00 KST` through the report generation time. The report must exclude the load-test model named `dummy-model` from discovery, aggregation, rankings, charts, tables, and embedded data.

The report is a point-in-time operational snapshot. It must not make network requests after it is generated and must not contain credentials, access tokens, user identifiers, prompts, request bodies, or raw log content.

## Deliverables

- Editable visualization fragment:
  `/Users/choikh/.codex/visualizations/2026/07/10/019f4a6d-d027-73f0-904a-1cef4e7228dc/alrouter-july-performance-benchmark.html`
- Standalone report:
  `docs/reports/alrouter-performance-benchmark-2026-07.html`
- The standalone report contains inline CSS, JavaScript, SVG charts, and a compact sanitized data snapshot.

## Reporting Period

- Intended calendar period: `2026-07-01 00:00:00 KST` through the generation timestamp.
- The performance API accepts an integer `hours` value and computes a rolling start time.
- Query `hours` is `ceil((generated_at - july_start) / 3600)`.
- This may include less than one hour before July 1. The report must show both the intended calendar period and the effective API query window, with the boundary error stated explicitly.
- Management-log queries use exact `start_timestamp` and `end_timestamp` values for the intended calendar period.

## Data Sources

### Primary performance data

1. `GET /api/perf-metrics/summary?hours={hours}` discovers models and supplies server-weighted model-level:
   - average total latency;
   - success rate;
   - average TPS.
2. `GET /api/perf-metrics?model={model}&hours={hours}` supplies active-group metrics and hourly series:
   - average TTFT;
   - average total latency;
   - success rate;
   - average TPS.

The report captures responses during generation and embeds only normalized metrics.

### Supporting log data

`GET /api/log/` with `type=2`, exact timestamps, and model/group filters supplies successful consumption-log counts. These counts are labeled `successful log count`; they are not presented as the request-count denominator used by performance metrics.

Raw log fields such as username, token name, request ID, upstream request ID, IP address, content, and `other` are never embedded in the report.

## Exclusion Contract

Normalize a model name with `trim().toLowerCase()`. Exclude it when the result is exactly `dummy-model`.

Apply the exclusion before:

- model-detail API requests;
- log-count requests;
- KPI calculations;
- chart datasets;
- outlier detection;
- tables;
- inline JSON serialization.

Verification must fail if case-insensitive `dummy-model` appears in visible report text or embedded benchmark data. A methodology sentence may state that the named load-test model was excluded; this is the only permitted occurrence in explanatory copy.

## Metric Semantics

### Model-level metrics

- Average total latency, success rate, and TPS come directly from the server-weighted summary endpoint.
- The summary endpoint does not expose model-level TTFT.
- Model-level TTFT is therefore reported as `mean group TTFT`: the arithmetic mean of positive `avg_ttft_ms` values from that model's active groups.
- The UI and methodology must use that exact label and must not imply request-weighting.
- If no group has a positive TTFT, show `N/A` and classify it as insufficient streaming data, not zero latency.

### Group-level metrics

Use values returned by the per-model performance endpoint without reweighting:

- average TTFT;
- average total latency;
- success rate;
- average TPS;
- hourly series.

### Portfolio summaries

Portfolio KPIs use medians across included models rather than unweighted arithmetic means:

- median mean-group TTFT over models with TTFT;
- median model average total latency;
- median model success rate;
- included model count.

Medians are labeled as model medians and never presented as request-weighted fleet metrics.

## Information Architecture

### 1. Report header

- `AlRouter Performance Benchmark` title;
- intended period and generated-at timestamp;
- effective query-window disclosure;
- data-source and exclusion badges;
- print action.

### 2. Portfolio summary

Four compact metrics:

- included models;
- median mean-group TTFT;
- median total latency;
- median success rate.

### 3. Latency comparison

A responsive horizontal paired-bar chart compares mean-group TTFT and average total latency by model. It defaults to the models with the highest average total latency and directly labels values. The search and group controls update the same chart rather than creating duplicate charts.

### 4. Reliability comparison

A horizontal success-rate chart uses a 0–100% scale with visible 95% and 99% reference lines. Color is paired with numeric labels and must not be the only status cue.

### 5. Group comparison

Small multiples compare `default`, `svip`, `vip`, and `auto` only when data exists. Each group shows TTFT, total latency, success rate, and TPS on consistent units.

### 6. Detailed benchmark table

Columns:

- model;
- group;
- average TTFT;
- average total latency;
- success rate;
- TPS;
- successful log count;
- hourly series-point count.

The table supports model-name search, group filtering, and sorting by latency, TTFT, success rate, TPS, or model name. The default sort is average total latency descending.

### 7. Outliers and methodology

Show data-derived lists for:

- highest average total latency;
- highest mean-group TTFT;
- lowest success rate;
- largest latency-minus-TTFT gap.

The methodology section explains data sources, period-boundary error, missing TTFT behavior, successful-log-count semantics, and the exclusion rule.

## Visual Direction

### Visual thesis

A restrained operations dossier: dense, calm, print-ready, and led by precise horizontal comparisons rather than decorative dashboard chrome.

### Content plan

1. Working header and scope
2. Portfolio medians
3. Latency and reliability comparisons
4. Group breakdown
5. Searchable detailed evidence
6. Methodology and caveats

### Interaction thesis

- Search, group, and sort controls update charts, outliers, and the table together.
- Bars and table rows use short restrained transitions when filters change.
- Hover and keyboard focus reveal exact values while all essential values remain visibly labeled.

Use one neutral visual system with a single primary accent. Avoid a marketing hero, card mosaics, gradients, ornamental imagery, remote fonts, and external chart dependencies.

## Accessibility and Responsive Behavior

- Semantic headings, form labels, tables, and buttons.
- SVG charts have accessible names and concise text alternatives.
- Controls retain native keyboard and focus behavior.
- Important values are always visible without hover.
- Layout works at 1440px, 736px, and 390px without horizontal page scrolling.
- Print CSS removes controls, preserves charts and tables, and avoids splitting primary table rows.
- Respect `prefers-reduced-motion`.

## Error and Missing-Data Handling

- Abort generation if the summary endpoint fails or returns malformed data.
- Continue with a visible per-model data-quality note if one model-detail request fails.
- Show `N/A` for missing TTFT/TPS; never substitute zero.
- Omit empty groups and empty charts.
- If no non-dummy models remain, generate a valid report containing the scope and an explicit no-data state.
- Escape all model and group labels before inserting them into HTML or SVG.

## Data Security

- Read `.env` credentials only inside the collection process.
- Never echo credential values or put them in command arguments that are logged where avoidable.
- Do not embed API response headers, raw errors, raw logs, prompts, user data, request IDs, channel keys, or access tokens.
- The final report contains only model names, group names, aggregate metrics, timestamps, counts, and methodology text.

## Verification

1. Validate every collected object against the expected shape and numeric ranges.
2. Assert the intended and effective periods are recorded.
3. Assert the normalized dataset contains no `dummy-model` entry.
4. Assert the standalone HTML contains no credentials or known secret values.
5. Parse inline JavaScript with Node to catch syntax errors and undefined top-level data references.
6. Render the fragment and standalone report in a browser.
7. Inspect desktop and mobile screenshots for clipping, overlap, missing labels, and blank charts.
8. Exercise search, group filter, sorting, and print mode.
9. Open the standalone file without network access and confirm all content and interactions still work.
10. Confirm `git diff` contains only the intended design/report artifacts and preserves pre-existing user changes.

## Non-goals

- No live dashboard or recurring data refresh.
- No production API or database schema changes.
- No request-level trace export.
- No provider quality claims beyond the captured period and available aggregate metrics.
- No synthetic load generation as part of report creation.
