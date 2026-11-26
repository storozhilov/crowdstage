#!/usr/bin/env bash
#
# Run MixerConsole unit and browser end-to-end tests inside the builder image.
#
# Four studio test publishers are started with ci/media-mixer/run_studio.sh so
# the Studio panel has enough live sources to fill its card row and exercise the
# carousel navigation. Three guest test publishers are started with
# ci/mosaic/run_guest.sh so the Guests carousel has enough sources to exercise
# its two-card row and navigation. Two ci/media-mixer/run_camera.sh instances
# create virtual cameras and stream test patterns through them so the browser
# tests can add and remove camera sources. The runtime image is pulled when
# missing.
#
# The stand, ControlPlane, MosaicComposer, MediaMixer, and MixerConsole must be
# deployed before this script runs, for example:
#   ./ci/dev.sh up --wait
#   ./ci/control-plane/deploy.sh
#   ./ci/mosaic/deploy.sh
#   ./ci/media-mixer/deploy.sh
#   ./ci/mixer-console/deploy.sh
#   ./ci/mixer-console/test.sh

set -Eeuo pipefail

# shellcheck source=ci/lib.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/../lib.sh"

BUILDER_IMAGE="$("$CI_DIR/ensure_builder.sh")"
readonly BUILDER_IMAGE
readonly STUDIO_PUBLISHER="$REPO_ROOT/ci/media-mixer/run_studio.sh"
readonly STUDIO_PUBLISH_TIMEOUT_SECONDS=300
readonly STUDIO_PUBLISHER_IDENTITY="mixer-console-test-$$"
readonly GUEST_PUBLISHER="$REPO_ROOT/ci/mosaic/run_guest.sh"
readonly GUEST_PUBLISH_TIMEOUT_SECONDS=300
readonly GUEST_PUBLISHER_IDENTITY="mixer-console-guest-$$"
readonly CAMERA_PUBLISHER="$REPO_ROOT/ci/media-mixer/run_camera.sh"
readonly CAMERA_PUBLISH_TIMEOUT_SECONDS=300
readonly CAMERA_COUNT="${MIXER_CONSOLE_TEST_CAMERA_COUNT:-2}"
readonly CAMERA_FIRST_VIDEO_NR="${MEDIA_MIXER_CAMERA_FIRST_VIDEO_NR:-10}"

studio_patterns=(smpte snow ball bar)
guest_patterns=(smpte snow ball)
camera_patterns=(smpte75 ball)
studio_publisher_pids=()
guest_publisher_pids=()
camera_publisher_pids=()
camera_devices=()
camera_device_args=()
mkdir -p "$NPM_CACHE_DIR"

studio_publisher_log="$(mktemp "${TMPDIR:-/tmp}/crowdstage-mixer-console-studio.XXXXXX")"
guest_publisher_log="$(mktemp "${TMPDIR:-/tmp}/crowdstage-mixer-console-guests.XXXXXX")"
camera_publisher_log="$(mktemp "${TMPDIR:-/tmp}/crowdstage-mixer-console-cameras.XXXXXX")"

# shellcheck disable=SC2317 # invoked by the EXIT trap
cleanup() {
  local pid
  if (( ${#studio_publisher_pids[@]} > 0 )); then
    for pid in "${studio_publisher_pids[@]}"; do
      kill -INT "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    done
  fi
  studio_publisher_pids=()
  if (( ${#guest_publisher_pids[@]} > 0 )); then
    for pid in "${guest_publisher_pids[@]}"; do
      kill -INT "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    done
  fi
  guest_publisher_pids=()
  if (( ${#camera_publisher_pids[@]} > 0 )); then
    for pid in "${camera_publisher_pids[@]}"; do
      kill -INT "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    done
  fi
  camera_publisher_pids=()
  rm -f "$studio_publisher_log" "$guest_publisher_log" "$camera_publisher_log"
}
trap cleanup EXIT

if ! docker_image_exists "$MEDIA_MIXER_STUDIO_IMAGE"; then
  info "Pulling studio publisher image $MEDIA_MIXER_STUDIO_IMAGE"
  docker pull "$MEDIA_MIXER_STUDIO_IMAGE"
fi

printf 'Starting %d studio test publishers\n' "${#studio_patterns[@]}"
for index in "${!studio_patterns[@]}"; do
  MEDIA_MIXER_STUDIO_IDENTITY="$STUDIO_PUBLISHER_IDENTITY-$((index + 1))" \
  MEDIA_MIXER_STUDIO_PATTERN="${studio_patterns[$index]}" \
    "$STUDIO_PUBLISHER" "$STUDIO_PUBLISH_TIMEOUT_SECONDS" \
    >>"$studio_publisher_log" 2>&1 &
  studio_publisher_pids+=("$!")
done

printf 'Starting %d guest test publishers\n' "${#guest_patterns[@]}"
for index in "${!guest_patterns[@]}"; do
  MOSAIC_GUEST_IMAGE="$MEDIA_MIXER_STUDIO_IMAGE" \
  MOSAIC_GUEST_IDENTITY="$GUEST_PUBLISHER_IDENTITY-$((index + 1))" \
  MOSAIC_GUEST_PATTERN="${guest_patterns[$index]}" \
    "$GUEST_PUBLISHER" "$GUEST_PUBLISH_TIMEOUT_SECONDS" \
    >>"$guest_publisher_log" 2>&1 &
  guest_publisher_pids+=("$!")
done

printf 'Starting %d camera test publishers\n' "$CAMERA_COUNT"
for (( index = 0; index < CAMERA_COUNT; index++ )); do
  MEDIA_MIXER_CAMERA_POOL_SIZE="$CAMERA_COUNT" \
  MEDIA_MIXER_CAMERA_PATTERN="${camera_patterns[$((index % ${#camera_patterns[@]}))]}" \
    "$CAMERA_PUBLISHER" "$CAMERA_PUBLISH_TIMEOUT_SECONDS" \
    >>"$camera_publisher_log" 2>&1 &
  camera_publisher_pids+=("$!")
done

for (( index = 0; index < CAMERA_COUNT; index++ )); do
  camera_devices+=("/dev/video$((CAMERA_FIRST_VIDEO_NR + index))")
done

camera_deadline=$((SECONDS + 30))
for device in "${camera_devices[@]}"; do
  while [[ ! -e "$device" ]]; do
    if (( SECONDS >= camera_deadline )); then
      err "Virtual camera device did not appear: $device"
      if [[ -s "$camera_publisher_log" ]]; then
        cat "$camera_publisher_log" >&2
      fi
      exit 1
    fi
    sleep 0.1
  done

  # Udev may still be applying ownership and mode changes when the device node
  # first appears. Wait for the attributes to settle before passing the device
  # to the browser test container.
  previous_attributes=""
  while true; do
    current_attributes="$(stat -c '%g:%a' "$device" 2>/dev/null || true)"
    if [[ -n "$current_attributes" && "$current_attributes" == "$previous_attributes" ]]; then
      break
    fi
    if (( SECONDS >= camera_deadline )); then
      err "Virtual camera permissions did not settle: $device"
      exit 1
    fi
    previous_attributes="$current_attributes"
    sleep 0.2
  done
done

# Make sure the browser test container can read the virtual cameras regardless
# of the host udev rules that created them.
if ! docker run --rm \
  --privileged \
  --network none \
  --user 0 \
  --mount "type=bind,source=/dev,target=/dev" \
  --entrypoint chmod \
  "$BUILDER_IMAGE" \
  666 "${camera_devices[@]}"; then
  err "Could not make the virtual cameras readable by the browser tests."
  exit 1
fi

for device in "${camera_devices[@]}"; do
  camera_device_args+=(
    --device "$device"
    --group-add "$(stat -c '%g' "$device")"
    --group-add 0
    --group-add 44
  )
done

printf 'Camera devices prepared for the browser tests:\n'
for device in "${camera_devices[@]}"; do
  info "  $device $(stat -c 'mode=%a uid=%u gid=%g' "$device")"
done

printf 'Running MixerConsole tests\n'
status=0
docker run \
  --rm \
  --init \
  --network host \
  --shm-size=1g \
  --mount "type=bind,source=$REPO_ROOT,target=/src" \
  --mount "type=bind,source=$NPM_CACHE_DIR,target=/cache/npm" \
  "${camera_device_args[@]}" \
  --env NPM_CONFIG_CACHE=/cache/npm \
  --env MIXER_CONSOLE_URL \
  --env CONTROL_PLANE_URL \
  --env MEDIA_MIXER_URL \
  --env LIVEKIT_URL \
  --env LIVEKIT_API_KEY \
  --env LIVEKIT_API_SECRET \
  --env "CONTROL_PLANE_STUDIO_ROOM=$CONTROL_PLANE_STUDIO_ROOM" \
  --env "CONTROL_PLANE_GUESTS_SOURCE_ROOM=$CONTROL_PLANE_GUESTS_SOURCE_ROOM" \
  --env MOSAIC_SOURCE_ROOM \
  --env MOSAIC_TARGET_ROOM \
  --env MEDIA_MIXER_SOURCE_ROOM \
  --env MEDIA_MIXER_TARGET_ROOM \
  --env MEDIA_MIXER_MOSAIC_ROOM \
  --env "STUDIO_PUBLISHER_IDENTITY=$STUDIO_PUBLISHER_IDENTITY" \
  --env "GUEST_PUBLISHER_IDENTITY=$GUEST_PUBLISHER_IDENTITY" \
  --workdir /src/mixer-console \
  "$BUILDER_IMAGE" \
  bash -c "npm ci && npm run test:unit && npm run test:e2e" || status=$?

if [[ "$status" -ne 0 && -s "$studio_publisher_log" ]]; then
  printf 'Studio test publisher log:\n'
  cat "$studio_publisher_log"
fi

if [[ "$status" -ne 0 && -s "$guest_publisher_log" ]]; then
  printf 'Guest test publisher log:\n'
  cat "$guest_publisher_log"
fi

if [[ "$status" -ne 0 && -s "$camera_publisher_log" ]]; then
  printf 'Camera test publisher log:\n'
  cat "$camera_publisher_log"
fi

exit "$status"
