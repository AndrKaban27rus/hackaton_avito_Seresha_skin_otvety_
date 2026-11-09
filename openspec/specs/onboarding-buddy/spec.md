# Capability: Onboarding Buddy

## Purpose
Help an intern find onboarding guidance and see the next step.

## Requirements

### Requirement: View onboarding progress
The system SHALL show the demo user's onboarding steps and completion state.

#### Scenario: Load checklist
- GIVEN the onboarding API is available
- WHEN the client requests the checklist
- THEN the API returns step IDs, titles, descriptions and status

### Requirement: Grounded answers
The assistant SHALL answer onboarding questions from approved content and return source references.

#### Scenario: Known answer
- GIVEN a question covered by a synthetic instruction
- WHEN the assistant responds
- THEN the response includes an answer and source

#### Scenario: No evidence
- GIVEN no approved source supports an answer
- WHEN the assistant responds
- THEN it does not invent a company policy and recommends the mentor

### Requirement: Explicit progress action
The system SHALL change a step only on explicit user action.

#### Scenario: Complete step
- GIVEN an onboarding step exists
- WHEN the user asks the API to complete it
- THEN the state is persisted and an onboarding event is emitted
