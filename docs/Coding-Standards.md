# BAOBAB Software Engineering & Coding Standards

> **Strong Roots. Inspired Growth.**

This document defines the software engineering and coding standards for the BAOBAB platform.

The objective of these standards is to promote the development of software that is secure, maintainable, scalable, testable, readable, and consistent across the entire platform.

As an enterprise-grade, polyglot software platform, BAOBAB brings together multiple programming languages, frameworks, and technologies. These standards establish a common engineering philosophy while recognising that each technology has its own language-specific conventions and best practices.

This document complements—not replaces—the official style guides and recommendations published by the maintainers of the technologies used within BAOBAB.

---

# Purpose

The purpose of these standards is to:

- Promote consistent engineering practices across the platform.
- Improve software quality and maintainability.
- Support secure software development.
- Encourage readable and well-structured code.
- Reduce technical debt.
- Improve collaboration between contributors.
- Simplify onboarding for new contributors.
- Support long-term sustainability of the project.

Every contribution should seek to leave the codebase in a better condition than it was found.

---

# Engineering Philosophy

BAOBAB is developed according to engineering principles that prioritise long-term quality over short-term convenience.

Our philosophy is guided by the belief that software should be:

- Simple where possible.
- Explicit rather than ambiguous.
- Secure by design.
- Easy to understand.
- Easy to test.
- Easy to maintain.
- Resilient under change.
- Designed for long-term evolution.

We value thoughtful engineering decisions that improve the platform over time rather than solutions that optimise only for immediate implementation.

---

# Engineering Principles

The following principles guide software development throughout BAOBAB.

## Readability

Code is read far more often than it is written.

Prioritise clarity over cleverness.

Write code that future contributors can understand with minimal effort.

---

## Simplicity

Choose the simplest solution that satisfies the requirements.

Avoid unnecessary abstraction, premature optimisation, and excessive complexity.

Simple systems are easier to understand, test, maintain, and extend.

---

## Consistency

Consistent code improves collaboration.

Follow established project conventions rather than introducing unnecessary variation in style, structure, or terminology.

Consistency should exist across services, applications, and documentation.

---

## Maintainability

Software should remain understandable and adaptable throughout its lifecycle.

Consider how future contributors will extend, troubleshoot, and improve the code.

Maintainability is a primary design objective—not an afterthought.

---

## Security by Design

Security should be considered from the earliest stages of design.

Do not rely solely on testing or later review to identify security weaknesses.

Secure defaults, least privilege, input validation, and defensive programming should be incorporated into every solution.

---

## Testability

Software should be designed to support effective automated and manual testing.

Loose coupling, clear interfaces, dependency injection where appropriate, and modular design improve testability.

Code that cannot be tested should be reconsidered.

---

## Scalability

Solutions should accommodate future growth without unnecessary redesign.

When making engineering decisions, consider future users, additional tenants, increased workloads, and expanding platform capabilities.

---

## Observability

Applications should provide sufficient logging, metrics, and tracing to support monitoring, troubleshooting, and operational insight.

Well-designed systems make it easier to understand behaviour in production.

---

## Automation

Where practical, repetitive engineering activities should be automated.

Automation improves consistency, reduces human error, and supports reliable delivery.

Examples include:

- Formatting
- Linting
- Testing
- Dependency management
- Continuous Integration
- Continuous Deployment

---

## Continuous Improvement

Software engineering is an ongoing process of refinement.

Every contributor is encouraged to improve:

- Code quality.
- Documentation.
- Test coverage.
- Performance.
- Security.
- Developer experience.

Small improvements made consistently produce significant long-term benefits.

---

# Supported Technologies

BAOBAB adopts a polyglot architecture that combines the strengths of multiple technologies.

The project currently uses—or intends to use—the following primary technologies:

| Domain | Technology |
|---------|------------|
| Control-plane runtime | Go (see [ADR-0001](adr/0001-go-control-plane-runtime.md)) |
| HTTP | `net/http` + `chi` |
| Database | PostgreSQL 17 |
| Containerisation | Docker |
| Infrastructure as Code | Terraform (`baobab-platform/infrastructure`) |
| Cloud Platform | AWS |
| Version Control | Git |
| CI/CD | GitHub Actions |

`baobab-platform/baobab-cp` is a single-language Go service; the wider Nabhold platform's other
repositories (digital estates, `baobab-trade`, `baobab-erp`, `baobab-pulse`) use their own
appropriate stacks and are governed by their own coding standards, not this document. See
the root [`README.md`](../README.md) for this repository's actual tech stack and structure.

---

# Relationship to Official Standards

BAOBAB follows recognised industry standards wherever practical.

Examples include:

| Technology | Primary Reference |
|------------|-------------------|
| Go | Effective Go, `gofmt`/`go vet`, and the Go standard project layout conventions |
| PostgreSQL | PostgreSQL documentation and SQL standards |
| Docker | Dockerfile best practices |
| Terraform | HashiCorp style guide |

Where this document provides additional guidance, it reflects project-specific engineering decisions intended to maintain consistency across BAOBAB.

---

# Scope

These standards apply to all source code contributed to BAOBAB, including:

- Backend services
- Frontend applications
- Mobile applications
- AI services
- Worker services
- Shared libraries
- Infrastructure as Code
- Build and deployment automation
- Utility scripts
- Configuration templates where appropriate

All contributors are expected to follow these standards unless an approved architectural decision explicitly requires an alternative approach.

---

---

# Architectural Consistency

BAOBAB follows a deliberate enterprise architecture supported by documented Architectural Decision Records (ADRs).

Contributors should:

- Follow established architectural patterns.
- Reuse existing components where appropriate.
- Avoid introducing new architectural styles without clear justification.
- Consider the impact of changes across the wider platform.
- Document significant architectural decisions through the ADR process.

Consistency across the platform improves maintainability, scalability, and developer productivity.

---

# Code Quality Standards

Every contribution should aim to improve the overall quality of the codebase.

Code should be:

- Correct.
- Readable.
- Maintainable.
- Testable.
- Secure.
- Efficient.
- Well documented.
- Reviewed before merging.

Code quality is measured by how well software continues to evolve over time—not merely by whether it compiles successfully.

---

# Project Organisation

Code should be organised according to the established repository structure.

Contributors should:

- Place functionality within the appropriate application or shared component.
- Avoid unnecessary duplication.
- Keep modules focused on a single responsibility.
- Separate business logic from infrastructure concerns.
- Maintain clear boundaries between platform components.

A predictable project structure improves discoverability and reduces maintenance effort.

---

# Single Responsibility Principle

Each module, class, function, and component should have one clear responsibility.

Avoid functions or classes that attempt to solve multiple unrelated problems.

Smaller, focused components are easier to:

- Understand.
- Test.
- Reuse.
- Maintain.
- Extend.

---

# Naming Conventions

Names should clearly communicate intent.

Prefer descriptive names over abbreviations.

Choose names that reflect the purpose of the code rather than its implementation.

Examples include:

### Variables

Prefer:

```python
customer_account
invoice_total
tenant_identifier
```

Instead of:

```python
ca
it
tid
```

---

### Functions

Function names should describe behaviour.

Examples:

```python
calculate_invoice_total()
validate_user_permissions()
generate_monthly_report()
```

---

### Classes

Class names should represent entities or responsibilities.

Examples:

```python
PaymentProcessor
TenantConfiguration
NotificationService
```

---

### Constants

Constants should be named consistently according to the conventions of the programming language being used.

For example:

```python
MAX_LOGIN_ATTEMPTS
DEFAULT_TIMEOUT_SECONDS
```

---

# Code Documentation

Code should be understandable without excessive comments.

When documentation is necessary:

- Explain *why*, not *what*.
- Document assumptions.
- Clarify complex algorithms.
- Describe non-obvious business rules.
- Keep documentation up to date.

Outdated comments are often more harmful than no comments.

---

# Self-Documenting Code

Good naming and thoughtful structure reduce the need for explanatory comments.

Prefer:

```python
calculate_discounted_price()
```

Instead of:

```python
calc()
```

Readable code is the first form of documentation.

---

# Error Handling

Errors should be anticipated and handled gracefully.

Contributors should:

- Handle expected exceptions explicitly.
- Avoid suppressing errors without justification.
- Provide meaningful error messages.
- Fail safely.
- Preserve useful diagnostic information.

Never expose sensitive implementation details to end users.

---

# Logging

Logging supports monitoring, troubleshooting, and operational visibility.

Logs should be:

- Accurate.
- Actionable.
- Consistent.
- Structured where appropriate.

Avoid logging:

- Passwords.
- Authentication tokens.
- API keys.
- Personal data unless operationally required and appropriately protected.

Logs should assist investigation without creating additional security risks.

---

# Configuration Management

Application behaviour should be configurable rather than hard-coded.

Configuration values should be managed through:

- Environment variables.
- Configuration files.
- Secret management services.
- Infrastructure configuration.

Avoid embedding environment-specific values directly within source code.

Examples include:

- Database connections.
- API endpoints.
- Cloud credentials.
- Feature flags.
- Service URLs.

Configuration should support development, testing, staging, and production environments consistently.

---

# Dependency Management

Dependencies should be introduced deliberately.

Before adding a new dependency, contributors should consider:

- Is similar functionality already available?
- Is the library actively maintained?
- Does it introduce unnecessary complexity?
- Does it have an acceptable security history?
- Is it compatible with the project's licence?

Minimising unnecessary dependencies reduces maintenance and security risks.

---

# Code Formatting

Code formatting should be applied consistently using automated tooling wherever practical.

Examples include:

| Technology | Recommended Tooling |
|------------|---------------------|
| Python | Ruff, Black |
| TypeScript | ESLint, Prettier |
| Flutter | `dart format` |
| Terraform | `terraform fmt` |

Automated formatting reduces subjective review comments and helps contributors focus on code quality rather than stylistic differences.

---

# Avoiding Technical Debt

Technical debt should be introduced only when there is a clear, documented justification.

Where technical debt cannot be avoided:

- Record the reason.
- Define a remediation plan.
- Track it through the project's issue management process.
- Avoid allowing temporary solutions to become permanent.

Long-term maintainability should always take precedence over short-term convenience.

---

# Consistency Before Cleverness

Elegant solutions are valuable, but predictability is more important than novelty.

Contributors should prefer approaches that are:

- Familiar to the team.
- Easy to understand.
- Well documented.
- Consistent with existing patterns.

Future maintainers should not need to reverse-engineer unnecessarily clever code.

---

> ---

# Secure Coding Practices

Security is a fundamental engineering requirement.

Contributors should:

- Validate all external input.
- Use parameterised database queries.
- Apply the Principle of Least Privilege.
- Enforce authentication and authorisation consistently.
- Protect sensitive information.
- Avoid exposing internal implementation details.
- Follow secure session management practices.
- Keep dependencies up to date.
- Address security findings promptly.

Security should be considered during design, implementation, review, and testing.

---

# Multi-Tenancy Standards

BAOBAB is designed as a multi-tenant enterprise platform.

Every contribution should preserve strict tenant isolation.

Contributors should ensure that:

- Every tenant-aware operation is explicitly scoped.
- Cross-tenant data access is prevented by design.
- Tenant context is consistently validated.
- Shared resources are carefully controlled.
- Background jobs execute within the correct tenant context.
- APIs enforce tenant boundaries.
- Audit logs include tenant identifiers where appropriate.

Tenant isolation is a core security and architectural requirement.

---

# Performance Guidelines

Performance should be considered throughout development.

Contributors should:

- Avoid unnecessary database queries.
- Optimise algorithms where appropriate.
- Minimise network requests.
- Cache responsibly.
- Use asynchronous processing when beneficial.
- Measure performance before optimising.
- Avoid premature optimisation.

Performance improvements should never compromise readability or correctness without clear justification.

---

# Database Standards

The database is a critical platform asset.

Contributors should:

- Design schemas carefully.
- Use meaningful table and column names.
- Apply appropriate indexes.
- Avoid unnecessary data duplication.
- Use transactions appropriately.
- Preserve referential integrity.
- Write efficient queries.
- Manage migrations carefully.

Database changes should be backwards compatible whenever practical.

---

# API Standards

APIs should be:

- Consistent.
- Well documented.
- Versioned appropriately.
- Secure.
- Predictable.

API development should include:

- Input validation.
- Meaningful error responses.
- Pagination where appropriate.
- Rate limiting.
- Authentication and authorisation.
- OpenAPI documentation.
- Backwards compatibility where practical.

Public APIs represent long-term contracts with consumers and should be designed accordingly.

---

# Artificial Intelligence Development Standards

AI functionality should be developed with the same engineering discipline as every other platform component.

Contributors should:

- Version prompts and prompt templates.
- Validate AI outputs where appropriate.
- Record model configurations.
- Evaluate model performance regularly.
- Protect sensitive information.
- Monitor hallucinations and unexpected behaviour.
- Implement human oversight for high-impact workflows.
- Document assumptions and limitations.

AI systems should be transparent, testable, and continuously evaluated.

---

# Retrieval-Augmented Generation (RAG)

Where Retrieval-Augmented Generation is implemented:

- Use trusted knowledge sources.
- Maintain versioned knowledge repositories.
- Validate retrieved content.
- Clearly distinguish retrieved information from generated content.
- Monitor retrieval quality over time.

The quality of AI responses depends on the quality of the underlying knowledge sources.

---

# Agentic AI Standards

Autonomous and semi-autonomous AI agents should:

- Operate within clearly defined boundaries.
- Use the minimum permissions required.
- Execute only authorised tasks.
- Maintain comprehensive audit logs.
- Fail safely.
- Escalate to human review when confidence thresholds are not met.

Agent behaviour should remain predictable, observable, and accountable.

---

# Infrastructure as Code (IaC)

Infrastructure should be managed using Infrastructure as Code wherever practical.

Infrastructure definitions should be:

- Version controlled.
- Reviewable.
- Reproducible.
- Secure.
- Tested where possible.

Changes to infrastructure should follow the same review process as application code.

---

# Container Development

Container images should:

- Use trusted base images.
- Minimise image size.
- Avoid unnecessary packages.
- Run with the least privileges required.
- Keep dependencies current.
- Be scanned regularly for known vulnerabilities.

Container security contributes directly to platform resilience.

---

# Cloud Engineering

Cloud resources should follow established organisational standards.

Contributors should:

- Apply least-privilege IAM policies.
- Encrypt data at rest and in transit.
- Use managed services where appropriate.
- Separate environments clearly.
- Monitor cloud resources continuously.
- Protect secrets using approved secret management solutions.

Infrastructure should remain portable, repeatable, and auditable.

---

# Observability Standards

Applications should support operational visibility through:

- Structured logging.
- Metrics collection.
- Distributed tracing where applicable.
- Health checks.
- Meaningful error reporting.

Observability enables effective troubleshooting and continuous improvement.

---

# Technical Documentation Within Code

Complex engineering decisions should be documented close to the implementation.

Examples include:

- Architectural assumptions.
- Business rules.
- Performance trade-offs.
- Security considerations.
- AI model constraints.

Documentation should explain reasoning rather than duplicate implementation details.

---

# Engineering Decisions

Where a significant technical decision affects multiple components or future development, contributors should consider creating or updating an Architectural Decision Record (ADR).

Examples include:

- Introducing a new framework.
- Changing architectural patterns.
- Adopting a new messaging strategy.
- Revising multi-tenancy approaches.
- Introducing new AI providers or model architectures.

Recording decisions helps preserve institutional knowledge and supports future contributors.

---

> ---

# Code Reviews

Every change to BAOBAB should undergo peer review before being merged into the main branch.

Code reviews are intended to:

- Improve software quality.
- Share knowledge across the team.
- Detect defects early.
- Ensure architectural consistency.
- Verify compliance with project standards.
- Encourage continuous learning.

Reviews should focus on improving the software, not criticising the contributor.

Constructive, respectful feedback strengthens both the codebase and the engineering community.

---

# Pull Request Standards

Each Pull Request should:

- Clearly describe the purpose of the change.
- Reference relevant Issues where applicable.
- Explain significant design decisions.
- Include appropriate documentation updates.
- Include tests for new functionality.
- Pass all required automated checks.
- Be appropriately scoped and focused.

Large Pull Requests should be divided into smaller, reviewable changes whenever practical.

---

# Quality Gates

Before a Pull Request can be approved and merged, it should satisfy the project's quality expectations.

Typical quality gates include:

## Code Quality

- Source code follows these Coding Standards.
- Formatting has been applied using approved tools.
- Linting completes successfully.
- Static analysis issues have been addressed.

---

## Testing

- Required automated tests pass.
- New functionality includes appropriate test coverage.
- Existing tests continue to pass.
- Regression testing has been considered where appropriate.

---

## Documentation

- Public APIs are documented.
- User-facing changes are reflected in documentation.
- Architecture documentation is updated where necessary.
- ADRs are created or amended for significant architectural decisions.

---

## Security

- Security-sensitive changes have been reviewed.
- Secrets are not committed to the repository.
- Dependencies have been evaluated.
- Security scanning completes successfully.

---

## Operational Readiness

Where applicable:

- Deployment considerations have been documented.
- Configuration changes are included.
- Monitoring requirements are addressed.
- Logging has been reviewed.
- Migration guidance has been prepared.

---

# Managing Technical Debt

Technical debt should be actively managed rather than ignored.

Where technical debt is introduced:

- Record the rationale.
- Create an issue for future remediation.
- Assess associated risks.
- Review outstanding technical debt regularly.

Temporary solutions should not become permanent through neglect.

---

# Refactoring

Contributors are encouraged to improve existing code while implementing new functionality.

Appropriate refactoring includes:

- Simplifying complex code.
- Improving readability.
- Reducing duplication.
- Strengthening modularity.
- Improving maintainability.

Refactoring should preserve existing behaviour unless an intentional functional change has been approved.

---

# Continuous Learning

Software engineering evolves continuously.

Contributors are encouraged to remain current with:

- Programming language developments.
- Framework updates.
- Security practices.
- Cloud technologies.
- Artificial Intelligence engineering.
- Testing methodologies.
- DevOps practices.

Continuous learning strengthens both individual contributors and the BAOBAB platform.

---

# Continuous Improvement

These standards are living documentation.

They will be reviewed periodically to reflect:

- Lessons learned.
- Technological advances.
- Community feedback.
- Architectural evolution.
- Security recommendations.
- Industry best practices.

Suggestions for improvement are welcome through the project's standard contribution process.

---

# Governance References

These Coding Standards should be read alongside the following project documents:

| Document | Purpose |
|----------|---------|
| `README.md` | Project vision, architecture, and repository overview. |
| `CONTRIBUTING.md` | Contribution workflow and contributor responsibilities. |
| `SECURITY.md` | Secure development and vulnerability disclosure policy. |
| `CODE_OF_CONDUCT.md` | Community expectations and professional conduct. |
| `CHANGELOG.md` | Release history and version management. |
| `docs/Testing-Standards.md` | Testing philosophy and quality assurance practices. |
| `docs/Documentation-Standards.md` | Documentation principles and maintenance expectations. |
| `docs/adr/` | Architectural Decision Records (ADRs). |

Together, these documents provide the governance framework that supports the development, operation, and long-term sustainability of BAOBAB.

---

# Final Statement

Engineering excellence is achieved through discipline, collaboration, and a commitment to continuous improvement.

These Coding Standards establish the expectations that guide software development throughout BAOBAB. By following them consistently, contributors help ensure that the platform remains secure, reliable, maintainable, scalable, and ready to evolve with changing technologies and business needs.

Every line of code contributes to the strength of the platform.

Every review strengthens its quality.

Every improvement supports its future.

---

<div align="center">

## Strong Roots. Inspired Growth.

**Engineering software with integrity, consistency, and excellence.**

</div>
