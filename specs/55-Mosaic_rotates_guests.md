Implement a guest rotation in mosaic tiles if their amount is bigger
then total cells in a grid.

Introduce a configurable timeout, so after it has expired the tile becomes the
candidate for the guest replace. Timeout could be passed to `POST /start`
REST endpoint as an optional JSON body attribute.  If omitted some default
value should be used.  `GET /health` should report this param.

Consider in E2e tests to start `MosaicComposer` on a small grid (e.g. 2x2 or
2x3 or whatever) so no need to run too many partipants to test this feature.
