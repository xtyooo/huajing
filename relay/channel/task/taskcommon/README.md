# Client media URL lists

Use `MediaURLList` for image, video and audio **list fields** in standard
client request DTOs. Plugins may send one URL as a JSON string or as a
single-element array; both must reach the same provider validation and mapping.
The reusable multipart decoder also turns a single text field into a string
and repeated text fields into an array.

- Accept string, string array, null, empty string and empty array.
- Preserve URL contents and array order (including signed query parameters).
- Do not split strings on commas or stringify objects, numbers or booleans.
- Reject non-string array elements.
- Keep provider URL, count, mode and model checks after decoding.
- Keep upstream DTOs as arrays; this compatibility is at the client boundary.
- Raw file upload paths and provider-specific multipart forwarding remain
  separate; this type only represents URL references.

Regression coverage includes every media list field on the seven standard
adaptors, signed URLs, invalid JSON types and single/repeated multipart fields.
