Install all neccessary GStreamer Go SDK related prequisites to the `builder` image.

Modify `MosaicComposer` so that it would copy guest's media stream to the
GStreamer pipeline which stores streaming data to the local file.

Implement an auto-test which verifies the guest's media is stored to file
by the GStreamer pipeline.
