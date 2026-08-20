# Technical Debt Register

This register contains known follow-up work that is intentionally outside the current memory and duplicate-record remediation scope.

| Priority | Debt | Current control | Exit criterion |
|---|---|---|---|
| High (operational) | Existing databases retain legacy Base64 image rows and payload-heavy `sync_queue` rows until the guarded migration is run. | `clipsync-migrate` defaults to a read-only plan; apply requires an exact database name and backup confirmation. A database startup gate rejects upgraded applications while old rows remain. | Both local and server databases report zero legacy image rows and zero pending/failed legacy queue rows; backup and migration reports are retained. |
| Medium | The compatibility sync API still Base64-encodes one image at a time, adding wire and transient allocation overhead. | Lists and events never carry image bodies; queue flush is serial and byte-bounded. | Introduce a versioned binary image upload/download endpoint, then retire data URLs after all clients migrate. |
| Medium | DIB ingestion intentionally rejects compressed/paletted clipboard formats outside 24/32-bit BI_RGB and BI_BITFIELDS. | Unsupported images fail explicitly and are not persisted as corrupt records. | Add corpus-backed decoders for required Windows formats with size and decompression-bomb limits. |
| Medium | Schema evolution is implemented by an idempotent guarded tool rather than a general versioned migration framework. | Migration code is isolated, resumable, integration-tested, and requires a backup gate. | Adopt numbered forward/rollback migrations before the next incompatible schema change. |
| Low | Android clipboard background access and image capture remain constrained by platform policy and currently support app-originated text suppression only. | The Android client follows the OS foreground-service model and does not claim unsupported capture behavior. | Define supported Android API levels and add device tests for image capture only if platform APIs permit a reliable implementation. |
