# PjPlan Aide Plugin

PjPlan provides a decimal-class catalogue lookup, a project filename builder, and starter templates for common project-control documents.

## Filename rule

`<decimal class> <document type> - <project identifier> - <track><major><minor>.<extension>`

Example: `102 62 Design Specification - 1234567 - PA1.md`.

- `P` is a review version and `R` is a release version.
- `A`, `B`, and `C` are major versions; the numeric suffix is the minor version.
- Configure project identifier and version fields in the plugin settings. Those settings are ordinary non-secret plugin configuration.
- The catalogue is derived from the supplied workbook's document-class sheet. Applicant names and application dates are not packaged. The class 1402 display name is generalized while its code is retained.

## Aide integration

The package targets Aide Plugin Protocol v1.2. It returns catalogue matches, filenames, and template text. Aide validates new document filenames and remains responsible for writes, sandbox enforcement, source visibility, and approval. Authored documents default to the independent `system-docs` source; other sources keep their configured AI visibility and read/write access.

## Tools

- `pjplan_class_lookup`: find document types by code, English title, or Chinese description.
- `pjplan_build_filename`: build a filename from a registered class and project settings.
- `pjplan_document_template`: return a starter template for project plan, progress report, verification plan, or milestone record.

The generated catalogue omits workbook applicant and date columns. It does not claim to be an authoritative external standard.
