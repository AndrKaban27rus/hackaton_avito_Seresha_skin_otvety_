# AI-buddy: feature specification

## User story
As an intern, I want a source-backed answer about my first weeks and a visible checklist, so I can take the next onboarding step without searching several systems.

## Scope
- Load three onboarding steps and current completion state.
- Ask one question and receive a Russian answer with approved source or mentor fallback.
- Explicitly complete a checklist step and persist the result.

## Out of scope
Real identity/HR integration, real employee data, autonomous actions, financial operations, broad onboarding administration.

## Acceptance criteria
- Checklist loads and reflects database state.
- Question response includes a source when available; otherwise says evidence is missing.
- Explicit completion survives an API restart and emits an onboarding event.
- UI displays pending/error states for remote requests.
