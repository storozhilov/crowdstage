Make broadcast stream to avoid video interruption on studio source switch.

Looks like there is no need to re-create a GST pipeline in MediaMixer each time
the source is switched. There should be a single long-running "dynamic"
GST-pipeline which should be properly adjusted each time the source is
switched.

I believe the same idea is already implemented in MosaicComposer, so take a
look how it's made there and borrow something if it makes sense.

Consider Go code de-dup where it makes sense.
