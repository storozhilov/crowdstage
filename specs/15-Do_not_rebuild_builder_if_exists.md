In `ci/ensure_builder.sh` do not build image if it exists on the host already.

Introduce a `-f|--force` CLI arg to the script which dictates an unconditional image build.
