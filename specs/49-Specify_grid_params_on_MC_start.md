Add support for two arguments to `POST /start` REST endpoint
of `MosaicComposer`:

- Mosaic grid column count;
- Mosaic grid line count.

Arguments should be passed to REST backend in HTTP-request
JSON body. If omitted, the default values for these params
are `4` and `3` respectively.
