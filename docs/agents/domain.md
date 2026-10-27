# Domain docs

**Layout: single-context.** This repo has one domain context.

- `GLOSSARY.md` at the repo root: the project's ubiquitous language (terms, their meaning, and terms to avoid).
- `docs/adr/`: Architecture Decision Records, one file per decision, numbered `NNNN-short-title.md`.

Neither exists yet; create them lazily the first time a skill needs to record a term or a decision.

## Consumer rules

- **Before designing, planning, or naming things**, read `GLOSSARY.md` if it exists and use its terms exactly. Don't invent synonyms for a defined term.
- **Before changing an area of the code**, skim `docs/adr/` for decisions that touch it. Don't silently contradict an accepted ADR; if the work requires reversing one, say so and propose a superseding ADR.
- **When a new domain term is settled** in conversation, add it to `GLOSSARY.md` (via the `domain-modeling` skill).
- **When a hard-to-reverse or non-obvious decision is made**, record it as a new ADR rather than burying it in a PR description.
- Missing files are not an error: absence just means nothing has been recorded yet.
