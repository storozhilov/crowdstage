# Roadmap

This is an indicative roadmap, not a delivery commitment.

## Milestone 0 — Repository and development baseline

- [x] Publish project overview and contribution guidelines
- [x] Establish initial development environment
- [ ] Add basic CI checks

## Milestone 1 — End-to-end video relay

- [ ] Browser A publishes a camera track to a LiveKit room
- [ ] Composer joins the room as a distinct participant
- [ ] Composer subscribes to one source video track
- [ ] Composer republishes the track under a stable output track name
- [ ] Browser B receives the composed/relayed output
- [ ] Handle source disconnect and reconnect
- [ ] Document manual test procedure

## Milestone 2 — First composition

- [ ] Receive multiple source tracks
- [ ] Compose a basic grid
- [ ] Publish the composed output
- [ ] Add basic layout configuration

## Milestone 3 — Studio workflow

- [ ] Guest list and preview
- [ ] Select guests for the programme
- [ ] Preview/Program workflow
- [ ] Full-screen Studio Display

## Milestone 4 — Production and broadcast integration

- [ ] Audio routing and mix-minus investigation
- [ ] OBS integration
- [ ] Broadcast output via HLS/LL-HLS
- [ ] Operational metrics and diagnostics

## Non-goals for the first milestone

- Supporting hundreds of simultaneous active cameras
- Full production automation
- Persistent user accounts and billing
- Multi-region deployment
