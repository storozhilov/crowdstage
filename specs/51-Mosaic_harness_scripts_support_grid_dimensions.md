Add support for grid dimensions to `MosaicComposer` harness scripts:

- `ci/mosaic/start.sh` should accept an optional param[s] with the grid
  dimensions e.g. in `5x2` or `5:2` or whatever format;
- `ci/mosaic/get_health.sh` should report grid dimensions if in running state.

Maybe you find something else - feel free to go ahead with the ideas.
