You are working on a project for an interactive online event platform featuring a video mosaic based on LiveKit and GStreamer.

## Task

Implement a Bash script toolset in the `ci/` directory to start, stop, and manage the project's local development environment using Docker Compose.

At this stage, you do NOT need to implement the application itself, business logic, Mosaic Composer, or client applications. The task is limited to infrastructure tooling for managing the development environment.

## 1. First, explore the project

Before making changes:

1. Study the repository structure.
2. Check if a Dockerfile, compose.yaml / docker-compose.yml, `.env`, Makefile, or other infrastructure configuration already exists.
3. Do not overwrite or delete existing files unless necessary.
4. If there is no Docker Compose configuration yet, prepare a minimal base configuration suitable for future expansion.

First, formulate a brief plan of changes, then proceed with the implementation.

## 2. Directory structure

Proposed structure:

ci/
  dev.sh
  compose/
    docker-compose.yml
  .env.example
  README.md

It is acceptable to slightly modify the structure if justified by the existing repository organization.

All paths must be calculated relative to the location of the script itself, not the user's current working directory.

## 3. Main management script

Create `ci/dev.sh`.

The script must support the following commands:

- `up` — create and start the development environment in the background.
- `down` — stop and remove the environment's containers and networks, preserving volumes.
- `restart` — restart the environment.
- `status` — show the status of services.
- `ps` — show Docker Compose containers.
- `logs [service]` — show logs for all services or a specific service.
- `follow [service]` — show logs in follow mode.
- `pull` — download necessary images.
- `build` — build images.
- `clean` — stop the environment and remove its volumes. Always request confirmation before execution.
- `help` — display help information.

For `logs` and `follow`, support an optional argument with the service name.

Examples:

    ./ci/dev.sh up
    ./ci/dev.sh status
    ./ci/dev.sh logs
    ./ci/dev.sh logs livekit
    ./ci/dev.sh follow
    ./ci/dev.sh down
    ./ci/dev.sh clean

## 4. Bash script requirements

- Use Bash compatible with Ubuntu 22.04/24.04.
- Include `set -Eeuo pipefail`.
- Handle errors with clear messages and a non-zero exit code.
- Check for the presence of Docker.
- Check for the presence of Docker Compose V2 (`docker compose`).
- Check for Docker daemon availability.
- If a necessary dependency is missing, output clear instructions.
- Support running from any current directory.
- Use `docker compose -f ... --project-directory ...` with absolute paths.
- Pass additional Docker Compose arguments if appropriate.
- Do not use `sudo` automatically.
- Do not hide Docker Compose errors.
- Make the script executable.

The Compose project name must be stable, e.g., `event-platform-dev`, so that running from different directories does not create different projects.

## 5. Docker Compose configuration

If there is no existing Compose configuration, create a minimal development environment with a single LiveKit service.

Requirements:

- Use the official LiveKit Docker image.
- Do not hardcode an obviously outdated version. Use the current stable version or a clearly defined version parameter.
- Move the configuration to a separate file.
- Make ports and parameters configurable via `.env`.
- Add a healthcheck if supported by the chosen image and it does not require additional utilities missing from the container.
- Do not add GStreamer, Mosaic Composer, PostgreSQL, Redis, or frontend yet — they will be implemented in subsequent stages.
- Do not use `latest` without explaining why.

If a full LiveKit configuration requires additional parameters, add a minimal LiveKit configuration file and mount it via a volume.

Provide a `.env.example` with explanations of the parameters.

## 6. Idempotency

Running any command multiple times should not lead to an incorrect state.

In particular:

- `up` can be executed multiple times.
- `down` is acceptable to execute even if the environment is not running.
- `restart` should work correctly when the environment is stopped.
- `status` should not fail just because the containers are not yet created.
- `clean` should only remove resources belonging to this specific Compose project.

## 7. Documentation

Create `ci/README.md` describing:

- environment requirements;
- initial setup;
- creating `.env` from `.env.example`;
- starting and stopping;
- viewing logs;
- cleaning the environment;
- location of the Compose configuration;
- purpose of environment variables.

The documentation must be in English.

## 8. Checks

After implementation:

1. Check Bash syntax using `bash -n`.
2. Check the behavior of the `help` command.
3. Check behavior when Docker or Docker Compose is missing (if possible via an isolated PATH or mock).
4. If Docker is available, verify the Compose configuration using `docker compose config`.
5. Do not run `clean` and do not delete user Docker volumes.
6. Do not execute commands that could affect other Docker Compose projects.

If a full environment startup is not possible in the current environment, explicitly state this in the report.

## 9. Limitations

- Do not install Docker or other system packages.
- Do not change host configuration.
- Do not use Kubernetes.
- Do not add a CI/CD pipeline.
- Do not implement unnecessary abstractions or a custom environment management framework.
- Do not add dependencies if the task can be solved using Bash and Docker Compose.

## 10. Readiness criteria

The result is considered ready if:

- `ci/dev.sh help` works.
- The script runs from any directory.
- `up`, `down`, `restart`, `status`, `logs`, `follow`, `pull`, `build`, `clean` are implemented.
- The Docker Compose project has a fixed name.
- `down` preserves volumes.
- `clean` removes only the current project's volumes and requires confirmation.
- Configuration and environment variables are separated from the management script.
- README contains usage instructions.
- Bash syntax and available checks pass.

## 11. Report

At the end:

1. List the created and modified files.
2. Briefly describe the implemented commands.
3. Indicate the results of the checks.
4. List known limitations.
5. Do not claim a successful LiveKit launch if you have not actually verified it.

Work in small increments. Do not try to implement the next components of the platform in advance.

