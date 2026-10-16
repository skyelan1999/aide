# PjPlan project document plugin

PjPlan provides decimal-class lookup, filename generation, and starter templates for common project-management documents. Configure it under **Settings → Plugins → PjPlan Project Documents**.

## Filename format

```text
<decimal class> <document type> - <project identifier> - <P|R><A|B|C><minor version>.<extension>
```

Example: `102 62 Design Specification - 1234567 - PA1.md`. `P` means review and `R` means release. `A`, `B`, and `C` are major versions; the numeric suffix is the minor version. Aide validates names for new documents. Existing attached files may keep their current names when edited.

The catalogue is derived from the workbook's `Decimal Class` sheet. The package contains only class codes, generalized English names, and Chinese descriptions; it omits applicant and application-date columns. Class 1402 keeps its code and uses the neutral name “Generic Configuration Management Rules.”

## Tools

- `pjplan_class_lookup`: search by class code, English title, or Chinese description.
- `pjplan_build_filename`: generate a filename from the selected class and plugin settings.
- `pjplan_document_template`: return starter Markdown for a project plan, progress report, verification plan, or milestone record; other classes receive a heading skeleton.

Choose a class before drafting. If the project identifier or version is missing, configure it in plugin settings instead of guessing.

## Aide storage and permissions

`write_file` routes new Markdown, text, CSV/JSON, PDF, Office, SVG, and draw.io documents to the independent `system-docs` source by default. It remains subject to the current workspace sandbox, source enablement, AI visibility, and host permissions. If no path is bound, Aide uses its private automatic-documents cache and never falls through to read-only `/context`. Other reference sources can be write targets only when AI-visible and configured as read/write. Existing files must be attached to the task before editing.

Code and engineering files continue to belong in the workspace. `write_file` creates an approval proposal; only an application receipt confirms that the document was saved.

## Package repository

The package source mirrors `git@github.com:skyelan1999/aide-pkg-PjPlan.git`. The remote repository was empty when inspected, so the initial package was authored against Aide Plugin Protocol v1.2. `aide/plugins/pjplan/` is the runtime copy.
