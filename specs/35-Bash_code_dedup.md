Move all _configuration envvars_ from Bash scripts to `ci/.env.example`.

In `ci/lib.sh` source `ci/.env` so all config envvars are to be available to
all scripts which source `ci/lib.sh`.

Source `ci/lib.sh` in all BASH scripts.

Move common code to `ci/lib.sh` as functions or config envvars where it makes
sense.
