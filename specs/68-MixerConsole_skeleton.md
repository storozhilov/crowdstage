Create a prototype of new component: `MixerConsole` - a web-application for
event host video/audio engineers to operate a platform either during a live
event or when doing some other work: maintenance, preparations, rehersals,
etc.

As for now `MixerConsole` should have just one _Control panel_ which is to be
used to operate a system. On the panel there should be two sub-panels/areas:
for `MediaMixer` and `MosaicComposer` respectively.  Each of this areas should
have the on/off switch (`POST /start`, `POST /stop`) and the parameters being
monitored (`GET /health`). These subpanels should be labeled as:

- **Media mixer**,
- **Mosaic composer**.

Web-application should call subsystems' REST API on the server side. It
provides it's own REST API which would be the only one that looks outside.
Other subsystems' (`MediaMixer`, `MosaicComposer`) REST API will not be
accessible from the outside.
