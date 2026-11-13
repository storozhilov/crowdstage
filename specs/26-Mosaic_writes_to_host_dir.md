Introduce following REST-endpoints to the `MosaicComposer`:

- `POST /start` - starts a media pipeline which streams media from the
  guest's LiveKit room to the studio LiveKit room and to the local file;
- `POST /stop` - stops a media pipeline.

Deploy `MosaicComposer` to the dev stand so it would write media to the
directory which is shared with the host's `ci/compose/recordings/` folder
which is gitignored.

Implement a test which is to verify `MosaicComposer` deployed to the stand
handles the REST-calls properly and writes media to files on host.

Set-up a user:group in `MosaicComposer` Docker image in the same manner as it's
done in `builder` image build script. No need to set up a user:group in Docker
compose configuration.
