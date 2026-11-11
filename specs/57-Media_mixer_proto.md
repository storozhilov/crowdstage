Implement a media mixer backend component prototype which as for now should
forward media which comes to studio media mix LiveKit room to the broadcast
media mix LiveKit room.

On `POST /start` the component should start a GST pipeline
which does the job.

On `POST /stop` GST pipeline is stopped.

On `GET /health` it should report an actual state data.

Create a respective `start.sh`/`get_health.sh`/`stop.sh` harness scripts.
