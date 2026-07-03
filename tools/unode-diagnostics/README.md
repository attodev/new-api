# unode Diagnostics Workbench

Local-only diagnostic UI for comparing direct unode requests and ALRouter routed requests.

## Run

```bash
cd tools/unode-diagnostics
npm test
npm start
```

Open the printed local URL.

## Safety

- The server reads the repo root `.env`.
- Request execution uses real keys only in server memory.
- History is written to `tools/unode-diagnostics/data/history.jsonl`.
- Stored history masks authentication headers and token-like body fields.
- Do not paste customer prompts or confidential customer data into diagnostics.

## Verification

```bash
cd tools/unode-diagnostics
npm test
npm start
```

Then open `http://127.0.0.1:5179`.

Run the four presets once and confirm:

- response summary is populated
- history persists after browser refresh
- clicking a history item restores the request and response panes
- `tools/unode-diagnostics/data/history.jsonl` contains masked auth values only
