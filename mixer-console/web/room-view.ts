import { Room, RoomEvent, Track } from 'livekit-client';

interface TokenResponse {
  url: string;
  room: string;
  identity: string;
  token: string;
}

export interface RoomViewOptions {
  statusSelector: string;
  tokenURL: string;
  connectingText: string;
  waitingText: string;
  liveText: string;
  errorText: string;
  playbackErrorText: string;
}

export interface SingleRoomViewOptions extends RoomViewOptions {
  videoSelector: string;
}

export interface CardRowViewOptions extends RoomViewOptions {
  cardRowSelector: string;
  slotCount?: number;
  onSelect?: (participantIdentity: string) => void;
}

export interface CardRowViewHandle {
  setSelectedParticipant(participantIdentity: string | null): void;
}

export interface RemoteVideoStream {
  track: Track;
  sid: string;
  participant: string;
  participantName: string;
}

interface RoomViewContext {
  play(video: HTMLVideoElement): void;
}

interface RoomVideoView {
  update(streams: RemoteVideoStream[], context: RoomViewContext): void;
}

interface CardRowSlot {
  element: HTMLDivElement;
  video: HTMLVideoElement;
}

const DEFAULT_CARD_SLOT_COUNT = 3;

function getElement<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing element: ${selector}`);
  }
  return element;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}

function collectRemoteVideoStreams(room: Room | null): RemoteVideoStream[] {
  if (!room) {
    return [];
  }

  const streams: RemoteVideoStream[] = [];
  for (const participant of room.remoteParticipants.values()) {
    for (const publication of participant.trackPublications.values()) {
      if (publication.kind === Track.Kind.Video && publication.track) {
        streams.push({
          track: publication.track,
          sid: publication.trackSid,
          participant: participant.identity,
          participantName: participant.name || participant.identity,
        });
      }
    }
  }
  return streams;
}

function connectRoomSession(options: RoomViewOptions, view: RoomVideoView): void {
  const status = getElement<HTMLParagraphElement>(options.statusSelector);

  let room: Room | null = null;
  let roomName = 'the LiveKit room';

  function setState(state: string, message: string): void {
    status.dataset.state = state;
    status.textContent = message;
  }

  function playVideo(video: HTMLVideoElement): void {
    void video.play().catch((error: unknown) => {
      if (isAbortError(error)) {
        return;
      }
      setState('error', `${options.playbackErrorText}: ${errorMessage(error)}`);
    });
  }

  const context: RoomViewContext = { play: playVideo };

  function refresh(removedTrack?: Track, removedSid?: string): void {
    const streams = collectRemoteVideoStreams(room).filter(
      (stream) => stream.track !== removedTrack && stream.sid !== removedSid,
    );

    view.update(streams, context);

    if (streams.length > 0) {
      setState('live', `${options.liveText} in ${roomName}`);
    } else {
      setState('waiting', `${options.waitingText} in ${roomName}...`);
    }
  }

  async function connect(): Promise<void> {
    setState('connecting', `${options.connectingText}...`);

    try {
      const response = await fetch(options.tokenURL);
      if (!response.ok) {
        throw new Error(`token request failed with status ${response.status}`);
      }

      const credentials = (await response.json()) as TokenResponse;
      if (!credentials.url || !credentials.token || !credentials.room) {
        throw new Error('token response is incomplete');
      }

      roomName = credentials.room;
      setState('connecting', `Connecting to ${roomName}...`);

      room = new Room({ adaptiveStream: false, dynacast: false });

      room.on(RoomEvent.TrackSubscribed, (track) => {
        if (track.kind === Track.Kind.Video) {
          refresh();
        }
      });

      room.on(RoomEvent.TrackUnsubscribed, (track) => {
        if (track.kind === Track.Kind.Video) {
          refresh(track);
        }
      });

      room.on(RoomEvent.TrackUnpublished, (publication) => {
        if (publication.kind === Track.Kind.Video) {
          refresh(undefined, publication.trackSid);
        }
      });

      room.on(RoomEvent.ParticipantDisconnected, () => {
        refresh();
      });

      room.on(RoomEvent.Reconnecting, () => {
        setState('reconnecting', `Reconnecting to ${roomName}...`);
      });

      room.on(RoomEvent.Reconnected, () => {
        refresh();
      });

      room.on(RoomEvent.Disconnected, (reason) => {
        setState('disconnected', `Disconnected from ${roomName} (${String(reason)})`);
      });

      await room.connect(credentials.url, credentials.token);

      if (status.dataset.state === 'connecting') {
        refresh();
      }
    } catch (error) {
      setState('error', `${options.errorText}: ${errorMessage(error)}`);
    }
  }

  void connect();
}

export function connectRoomView(options: SingleRoomViewOptions): void {
  const video = getElement<HTMLVideoElement>(options.videoSelector);
  let attached: Track | null = null;

  connectRoomSession(options, {
    update(streams, context): void {
      const next = streams[0]?.track ?? null;
      if (next === attached) {
        return;
      }

      if (attached) {
        attached.detach(video);
      }
      attached = next;

      if (next) {
        next.attach(video);
        context.play(video);
      }
    },
  });
}

function compareStreams(left: RemoteVideoStream, right: RemoteVideoStream): number {
  const leftKey = `${left.participant}\u0000${left.track.source}\u0000${left.sid}`;
  const rightKey = `${right.participant}\u0000${right.track.source}\u0000${right.sid}`;
  if (leftKey < rightKey) {
    return -1;
  }
  if (leftKey > rightKey) {
    return 1;
  }
  return 0;
}

function createCardSlots(root: HTMLElement, count: number): CardRowSlot[] {
  const slots: CardRowSlot[] = [];

  for (let index = 0; index < count; index += 1) {
    const element = document.createElement('div');
    element.className = 'card-row-slot';
    element.dataset.empty = 'true';

    const video = document.createElement('video');
    video.autoplay = true;
    video.playsInline = true;
    video.muted = true;

    element.append(video);
    root.append(element);
    slots.push({ element, video });
  }

  return slots;
}

export function connectCardRowView(options: CardRowViewOptions): CardRowViewHandle {
  const cardRow = getElement<HTMLElement>(options.cardRowSelector);
  const slotsRoot = getElement<HTMLElement>(
    `${options.cardRowSelector} [data-role="slots"]`,
  );
  const previous = getElement<HTMLButtonElement>(
    `${options.cardRowSelector} [data-role="previous"]`,
  );
  const next = getElement<HTMLButtonElement>(
    `${options.cardRowSelector} [data-role="next"]`,
  );
  const slotCount = options.slotCount ?? DEFAULT_CARD_SLOT_COUNT;
  const slots = createCardSlots(slotsRoot, slotCount);
  slotsRoot.style.setProperty('--card-row-slot-count', String(slotCount));

  let streams: RemoteVideoStream[] = [];
  let windowStart = 0;
  let context: RoomViewContext | null = null;
  let selectedParticipant: string | null = null;
  const attached: Array<Track | null> = slots.map(() => null);

  function updateNavigation(): void {
    const lastStart = Math.max(0, streams.length - slots.length);
    const hasOverflow = streams.length > slots.length;

    previous.hidden = !hasOverflow;
    next.hidden = !hasOverflow;
    previous.disabled = windowStart <= 0;
    next.disabled = windowStart >= lastStart;
    cardRow.dataset.count = String(streams.length);
  }

  function revealSelectedParticipant(): void {
    if (selectedParticipant === null) {
      return;
    }

    const index = streams.findIndex(
      (stream) => stream.participant === selectedParticipant,
    );
    if (index < 0) {
      return;
    }

    if (index < windowStart) {
      windowStart = index;
    } else if (index >= windowStart + slots.length) {
      windowStart = index - slots.length + 1;
    }
  }

  function render(): void {
    if (!context) {
      return;
    }

    const lastStart = Math.max(0, streams.length - slots.length);
    windowStart = Math.min(Math.max(windowStart, 0), lastStart);

    slots.forEach((slot, index) => {
      const stream = streams[windowStart + index];
      const nextTrack = stream?.track ?? null;

      if (attached[index] !== nextTrack) {
        if (attached[index]) {
          attached[index].detach(slot.video);
        }
        attached[index] = nextTrack;

        if (nextTrack) {
          nextTrack.attach(slot.video);
          context?.play(slot.video);
        }
      }

      slot.element.dataset.empty = stream ? 'false' : 'true';
      slot.video.hidden = !stream;

      if (stream) {
        const selected = stream.participant === selectedParticipant;
        slot.element.dataset.participant = stream.participant;
        slot.element.dataset.trackSid = stream.sid;
        slot.element.dataset.selected = selected ? 'true' : 'false';
        slot.element.dataset.selectable = selected ? 'false' : 'true';
        slot.element.tabIndex = selected ? -1 : 0;
        slot.element.setAttribute('role', 'button');
        slot.element.setAttribute(
          'aria-label',
          `${stream.participantName}'s stream${selected ? ' (selected)' : ''}`,
        );
      } else {
        delete slot.element.dataset.participant;
        delete slot.element.dataset.trackSid;
        delete slot.element.dataset.selected;
        delete slot.element.dataset.selectable;
        slot.element.dataset.empty = 'true';
        slot.element.removeAttribute('role');
        slot.element.removeAttribute('tabindex');
        slot.element.removeAttribute('aria-label');
      }
    });

    updateNavigation();
  }

  function selectSlot(index: number): void {
    const stream = streams[windowStart + index];
    if (!stream || stream.participant === selectedParticipant) {
      return;
    }

    if (options.onSelect) {
      options.onSelect(stream.participant);
      return;
    }

    selectedParticipant = stream.participant;
    render();
  }

  slots.forEach((slot, index) => {
    slot.element.addEventListener('click', () => {
      selectSlot(index);
    });
    slot.element.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        selectSlot(index);
      }
    });
  });

  previous.addEventListener('click', () => {
    if (windowStart > 0) {
      windowStart -= 1;
      render();
    }
  });

  next.addEventListener('click', () => {
    const lastStart = Math.max(0, streams.length - slots.length);
    if (windowStart < lastStart) {
      windowStart += 1;
      render();
    }
  });

  connectRoomSession(options, {
    update(nextStreams, nextContext): void {
      streams = [...nextStreams].sort(compareStreams);
      context = nextContext;
      revealSelectedParticipant();
      render();
    },
  });

  return {
    setSelectedParticipant(participantIdentity: string | null): void {
      if (selectedParticipant === participantIdentity) {
        return;
      }
      selectedParticipant = participantIdentity;
      revealSelectedParticipant();
      render();
    },
  };
}
