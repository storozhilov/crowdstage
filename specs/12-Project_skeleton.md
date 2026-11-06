Create a minimal Go project skeleton.

Requirements:
- Include the smallest possible Go application with a working `main` package.
- Place go-related stuff to a subdirectory.
- Provide a Docker-based `builder` image containing the Go toolchain.
- Script the builder image creation (`ci/ensure_builder.sh`) so where non-root
  user/group setup is done to be used by default in containers spawn up from the
  image.
- Implement `ci/build.sh` to build the Go application inside the builder container.
- Persist the Go module cache and build cache on the host between builds.
- Do not require Go or other project build dependencies on the host (only Docker and Docker Compose).
- Add a GitHub Actions workflow that builds the project using `ci/build.sh`.
- Follow existing repository conventions and keep everything minimal.

Use TDD where applicable. Verify the complete build locally and ensure CI runs the same build script. Do not add unnecessary frameworks, dependencies, or abstractions.
