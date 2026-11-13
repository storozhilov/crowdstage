# AGENTS.md

# CrowdStage — Engineering Principles and Development Rules

This document defines mandatory engineering principles, development practices, and expectations for all contributors and AI coding agents working on CrowdStage.

These rules apply to the entire repository unless a more specific `AGENTS.md` in a subdirectory explicitly overrides them.

The primary goal is to build a reliable, maintainable product through small, well-tested, understandable steps — not to build the most sophisticated architecture possible.

---

## 1. Core Engineering Principles

Follow these principles in all design and implementation decisions:

- **YAGNI** — You Aren't Gonna Need It. Do not implement functionality, abstractions, configuration options, or infrastructure that is not required by current, explicitly stated requirements.
- **KISS** — Keep It Simple, Stupid. Prefer the simplest solution that correctly solves the problem.
- **DRY** — Don't Repeat Yourself. Avoid unnecessary duplication, but do not introduce complex abstractions merely to eliminate a few similar lines of code.
- **SOLID** — Apply SOLID principles where they improve maintainability. Do not treat them as a reason to introduce unnecessary layers or interfaces.
- **Separation of Concerns** — Keep responsibilities clear and components reasonably independent.
- **Fail Fast** — Detect invalid states and configuration errors early. Provide clear, actionable error messages.
- **Convention over Configuration** — Prefer sensible defaults and established project conventions over excessive configurability.

### General rule

Prefer straightforward, explicit, boring code over clever, generic, or overly abstract solutions.

A junior engineer should be able to understand the implementation without extensive explanations or knowledge of the entire system.

Do not sacrifice simplicity for hypothetical future requirements.

---

## 2. Mandatory Test-Driven Development (TDD)

**TDD is mandatory. No production code should be written before a corresponding failing test exists.**

Follow the Red-Green-Refactor cycle:

1. **RED** — Write a test that describes the required behavior and fails for the expected reason.
2. **GREEN** — Implement the minimum amount of production code needed to make the test pass.
3. **REFACTOR** — Improve the implementation while keeping all tests passing.

Repeat this cycle in small increments.

### Rules

- Tests define and document expected behavior.
- Never write implementation first and add tests afterwards as an afterthought.
- Do not weaken, remove, skip, or modify a test merely to make an implementation pass.
- If a test exposes a bug, fix the underlying problem rather than hiding the failure.
- Tests must verify observable behavior, not implementation details.
- Tests should be deterministic and reproducible.
- Avoid arbitrary sleeps and timing-dependent assertions. Use bounded polling and explicit readiness conditions instead.
- Every bug fix should include a regression test.

If existing code has no tests, introduce the minimum necessary test infrastructure before extending that code.

### Test execution

All tests must run through the project's Docker-based development environment.

Do not require developers to install language runtimes, databases, test frameworks, or other project dependencies directly on the host.

---

## 3. Prefer End-to-End Tests

**Prefer tests that verify complete user-visible behavior across the entire application stack.**

The primary testing strategy should be built around realistic scenarios that exercise actual component interactions.

For example, in CrowdStage:

- A guest opens the application.
- The guest connects to the media server.
- The guest publishes a camera stream.
- The studio receives the stream.
- The operator changes the scene.
- The studio display receives the updated composition.

Whenever practical, test such behavior through real application interfaces and real running services.

### Testing pyramid and priorities

Prioritize tests in this order:

1. **End-to-End tests** — Verify complete user scenarios across the stack.
2. **Integration tests** — Verify interactions between real components and external services.
3. **Unit tests** — Verify isolated business logic and algorithms.

This does not mean unit tests are unnecessary. Use them where they provide fast, meaningful feedback, especially for complex logic and edge cases.

### Important rules

- Do not replace an integration test with a collection of mocked unit tests when real integration can reasonably be tested.
- Avoid excessive mocking of core application behavior.
- Prefer real infrastructure running in Docker over elaborate test doubles.
- Test public interfaces and observable outcomes.
- Keep the test suite as close to actual production behavior as reasonably possible.

Tests should give confidence that the system works, not merely that individual functions return expected values.

---

## 4. Docker-First Development Environment

**The host machine must remain as clean and lightweight as possible.**

The only required project development infrastructure installed on the host should be:

- Docker Engine.
- Docker Compose V2 plugin (`docker compose`).

Git may be used for repository management.

Do not require developers to install the following directly on the host:

- Programming language runtimes.
- Compilers and build toolchains.
- Package managers.
- Databases.
- Message brokers.
- Web servers.
- Test frameworks.
- Project-specific CLI tools.
- Other application dependencies.

All these tools must run inside Docker containers.

### Docker rules

- Use Docker Compose as the standard way to run the development environment.
- Build and test the application inside containers.
- Run linters, formatters, code generators, migrations, and other development tools inside containers.
- Automatically pull required images or build them when needed.
- Do not assume that a developer has previously installed or configured any project dependency.
- Provide a single documented and reproducible way to start, stop, inspect, and test the development environment.

Whenever possible, expose common operations through project scripts rather than requiring developers to remember long Docker commands.

For example:

```bash
./ci/dev.sh up
./ci/dev.sh down
./ci/dev.sh status
./ci/dev.sh logs
./ci/test.sh
```

Adapt these examples to the actual repository structure. Do not create redundant scripts if equivalent functionality already exists.

### Container image management

- Pin image versions. Avoid floating `latest` tags for reproducible development and CI.
- Prefer official and well-maintained base images.
- Keep images reasonably small.
- Use multi-stage builds where they provide a clear benefit.
- Avoid unnecessary containers and services.
- Do not install development tools into the host merely because installing them in a container seems inconvenient.

---

## 5. Offline and Poor-Connectivity Support

Development must be possible in environments with slow, unreliable, or temporarily unavailable Internet access.

All external container images used by the project must be discoverable and pre-downloadable.

Provide a convenient mechanism to:

1. Pull all images required by the development environment.
2. Build locally buildable project images.
3. Optionally export images into an archive.
4. Import previously exported images on another machine.

For example, a project may provide commands similar to:

```bash
./ci/dev.sh pull
./ci/dev.sh build
./ci/images.sh export ./images.tar
./ci/images.sh import ./images.tar
```

The actual implementation should match the repository's needs.

### Requirements

- Maintain a clear inventory of externally sourced images.
- Include images referenced by Docker Compose and Dockerfiles.
- Include images required by development and CI, where applicable.
- Do not silently depend on an image being available from the Internet at container startup.
- Avoid unnecessary image downloads.
- Document which images are pulled and which are built locally.

The goal is to allow a developer to prepare the environment while Internet access is available and continue working later without relying on network availability.

This requirement concerns container images and their dependencies. It does not imply that every external service or package registry must be mirrored.

---

## 6. Incremental Development

**Develop the project through small, independently verifiable steps.**

Do not attempt to implement a large feature, subsystem, or architectural design in one operation.

For each task:

1. Understand the existing implementation and repository structure.
2. Identify the smallest useful next increment.
3. Clarify ambiguous or missing requirements.
4. Define observable acceptance criteria.
5. Write a failing test.
6. Implement the minimum required functionality.
7. Run the relevant tests.
8. Refactor if necessary.
9. Update documentation where appropriate.

### Evolving requirements

Requirements may become more precise as working software is demonstrated and evaluated.

This is expected and should be supported by the development process.

Do not attempt to anticipate every possible future requirement.

When requirements change:

- Re-evaluate the current implementation.
- Preserve working behavior unless explicitly instructed otherwise.
- Make the smallest necessary change.
- Update affected tests and documentation.

### Scope discipline

Do not introduce unrelated improvements while implementing a task.

If you discover an unrelated problem:

- Mention it.
- Explain its impact.
- Suggest a separate follow-up task.

Do not silently expand the scope of the current change.

---

## 7. No Premature Optimization

**Do not optimize what has not been demonstrated to be a problem.**

Start with the simplest correct implementation.

Before introducing performance-oriented complexity:

1. Identify a concrete performance requirement or problem.
2. Establish a measurable baseline.
3. Determine the actual bottleneck.
4. Implement the smallest targeted optimization.
5. Measure the result.
6. Keep the optimization only if it provides a meaningful improvement.

Avoid:

- Premature caching.
- Unnecessary asynchronous processing.
- Unnecessary concurrency.
- Complex buffering strategies.
- Speculative sharding and partitioning.
- Unnecessary microservices.
- Complicated resource-management systems.
- Premature abstraction for hypothetical scaling requirements.

Performance matters, especially for a real-time media platform, but optimization must be driven by measurements and actual requirements.

Do not confuse architectural complexity with scalability.

---

## 8. Architecture and Dependencies

Prefer a small number of clearly defined components with explicit responsibilities.

### Rules

- Do not introduce a new dependency without a concrete reason.
- Prefer existing project dependencies over adding another library for a trivial task.
- Prefer mature, actively maintained, well-documented technologies.
- Avoid unnecessary frameworks.
- Avoid unnecessary wrappers around existing APIs.
- Do not create generic plugin systems, providers, registries, or dependency injection frameworks without an actual requirement.
- Keep interfaces as small as possible.
- Keep configuration explicit and understandable.
- Avoid duplicated sources of truth.
- Do not introduce distributed infrastructure when a single process or service is sufficient.

### Architectural decisions

When an architectural decision has significant long-term consequences, document it in an ADR (Architecture Decision Record).

An ADR should explain:

- The problem.
- The relevant constraints.
- The considered alternatives.
- The chosen solution.
- The reasons for the decision.
- Known trade-offs.

Do not create ADRs for trivial implementation details.

---

## 9. Code Quality and Maintainability

Code must be easy to read, understand, debug, and modify.

### Requirements

- Use clear and descriptive names.
- Keep functions and methods focused.
- Prefer explicit control flow.
- Keep nesting shallow.
- Avoid unnecessary indirection.
- Avoid clever one-liners when straightforward code is clearer.
- Handle errors explicitly.
- Do not silently ignore failures.
- Avoid global mutable state unless justified.
- Keep comments focused on explaining why, not restating what the code does.
- Remove dead code and unused dependencies.
- Follow established language-specific conventions.

### Complexity

Do not introduce abstractions merely because they might be useful someday.

A small amount of duplication is often preferable to an unnecessary abstraction.

A developer unfamiliar with the codebase should be able to follow the main execution path without jumping through many layers.

---

## 10. Configuration and Secrets

- Provide sensible development defaults.
- Document all non-obvious configuration parameters.
- Provide `.env.example` or equivalent templates where appropriate.
- Never commit secrets, credentials, private keys, or production tokens.
- Do not use production credentials in development or CI.
- Validate required configuration at startup.
- Fail with actionable error messages when configuration is invalid.
- Avoid unnecessary configuration options.

Configuration should describe actual requirements, not expose every internal implementation detail.

---

## 11. CI and Reproducibility

The CI pipeline must verify that the project can be built and tested from a clean environment.

CI should:

- Use pinned or otherwise reproducible tool versions where practical.
- Use the same Docker-based workflows as local development.
- Run the relevant automated tests.
- Verify application startup and readiness.
- Exercise important end-to-end scenarios.
- Report useful diagnostic information when something fails.
- Clean up temporary resources even after failures.

Do not maintain a separate, fundamentally different implementation of the development environment for CI.

Local development and CI should share the same scripts and configuration wherever practical.

---

## 12. Documentation

Documentation is part of the implementation, not an optional afterthought.

Keep documentation concise, accurate, and useful.

At minimum, document:

- What the project does.
- How to start and stop the development environment.
- How to run tests.
- How to build the application.
- How to pre-download required images.
- How to configure the environment.
- How to troubleshoot common problems.

Update documentation when behavior, commands, configuration, or architecture changes.

Do not document hypothetical functionality that has not been implemented.

---

## 13. Rules for AI Coding Agents

AI agents must follow these additional rules.

### Before changing code

- Inspect the relevant source files and existing project structure.
- Read the applicable `AGENTS.md` files.
- Understand existing conventions and reuse existing infrastructure.
- Check whether the requested functionality already exists.
- Identify the relevant tests and test execution commands.
- Do not assume that an earlier design discussion describes the current implementation.

### While implementing

- Work incrementally.
- Follow TDD.
- Make the smallest change that satisfies the current requirement.
- Do not modify unrelated files.
- Do not introduce dependencies without justification.
- Do not replace working code with a speculative redesign.
- Do not silently change public APIs or existing behavior.
- Do not claim that a test passed unless it was actually executed and passed.

### When requirements are unclear

Ask a concise clarification question when ambiguity materially affects the implementation.

If the ambiguity is minor, choose the simplest reasonable interpretation and state the assumption.

Do not invent requirements.

### Before finishing

- Run the relevant tests.
- Run formatting and static analysis where available.
- Check the final diff.
- Remove temporary files and debugging artifacts.
- Update documentation if necessary.
- Report exactly what was changed.
- Report which tests were actually executed and their results.
- Clearly disclose anything that remains untested or incomplete.

Never claim successful implementation based solely on code inspection.

---

## 14. Definition of Done

A task is complete only when all applicable conditions are met:

- [ ] The implementation satisfies the explicitly stated requirements.
- [ ] Tests were written before the corresponding production code.
- [ ] Relevant tests pass.
- [ ] Important user-visible behavior is covered by end-to-end or integration tests where practical.
- [ ] No unrelated functionality was introduced.
- [ ] No unnecessary dependencies or host installations were added.
- [ ] The development environment remains reproducible through Docker.
- [ ] Documentation was updated where necessary.
- [ ] The final changes were reviewed for unnecessary complexity.
- [ ] The actual test results and remaining limitations are reported.

---

## 15. Final Principle

**Build the smallest working thing, prove that it works, and only then make it more capable.**

Prefer working software over speculative architecture.

Prefer real tests over assumptions.

Prefer understandable code over clever code.

Prefer measured improvements over premature optimization.

Every development step should leave the project in a working, testable, and understandable state.
