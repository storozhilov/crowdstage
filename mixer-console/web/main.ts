interface Parameter {
  name: string;
  value: string;
}

interface SubsystemStatus {
  id: string;
  title: string;
  reachable: boolean;
  status: string | null;
  state: string | null;
  parameters: Parameter[];
  error?: string;
}

interface StatusResponse {
  status: string;
  subsystems: SubsystemStatus[];
}

interface ActionResponse {
  status: string;
  subsystem?: {
    id: string;
    title: string;
    ok: boolean;
    error?: string;
    response?: { status?: string };
  };
}

interface Panel {
  id: string;
  title: string;
  card: HTMLElement;
  stateBadge: HTMLElement;
  healthBadge: HTMLElement;
  switchButton: HTMLButtonElement;
  switchLabel: HTMLElement;
  parameters: HTMLElement;
  error: HTMLElement;
}

const POLL_INTERVAL_MS = 2000;
const PANEL_IDS = ['media-mixer', 'mosaic-composer'];

const message = getElement<HTMLParagraphElement>('#message');
const panels = PANEL_IDS.map((id) => createPanel(id));
const pending = new Set<string>();

let statusInFlight = false;

function getElement<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing element: ${selector}`);
  }
  return element;
}

function requireElement<T extends Element>(root: ParentNode, selector: string): T {
  const element = root.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing element: ${selector}`);
  }
  return element;
}

function createPanel(id: string): Panel {
  const card = getElement<HTMLElement>(`.subpanel[data-id="${id}"]`);

  return {
    id,
    title: requireElement<HTMLElement>(card, 'h2').textContent ?? id,
    card,
    stateBadge: requireElement<HTMLElement>(card, '[data-role="state"]'),
    healthBadge: requireElement<HTMLElement>(card, '[data-role="health"]'),
    switchButton: requireElement<HTMLButtonElement>(card, '[data-role="switch"]'),
    switchLabel: requireElement<HTMLElement>(card, '[data-role="label"]'),
    parameters: requireElement<HTMLElement>(card, '[data-role="parameters"]'),
    error: requireElement<HTMLElement>(card, '[data-role="error"]'),
  };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function setSwitch(panel: Panel, state: string, label: string): void {
  const isOn = state === 'on';
  panel.switchButton.dataset.state = state;
  panel.switchButton.setAttribute('aria-checked', isOn ? 'true' : 'false');
  panel.switchButton.disabled = state !== 'on' && state !== 'off';
  panel.switchLabel.textContent = label;
}

function switchState(state: string | null): string {
  if (state === 'running') {
    return 'on';
  }
  if (state === 'idle') {
    return 'off';
  }
  return 'unknown';
}

function switchLabel(title: string, state: string): string {
  if (state === 'on') {
    return `${title} on`;
  }
  if (state === 'off') {
    return `${title} off`;
  }
  return `${title} state unknown`;
}

function showMessage(text: string, kind: 'info' | 'error'): void {
  message.dataset.kind = kind;
  message.textContent = text;
}

function clearMessage(): void {
  message.dataset.kind = 'info';
  message.textContent = '';
}

function renderStatus(status: StatusResponse): void {
  document.body.dataset.status = status.status;

  for (const panel of panels) {
    const subsystem = status.subsystems.find((entry) => entry.id === panel.id);
    renderSubsystem(panel, subsystem);
  }
}

function renderSubsystem(panel: Panel, subsystem: SubsystemStatus | undefined): void {
  if (!subsystem) {
    renderUnavailable(panel, 'subsystem status is missing');
    return;
  }

  const state = subsystem.state ?? (subsystem.reachable ? 'unknown' : 'unreachable');
  const health = subsystem.reachable ? subsystem.status ?? 'unknown' : 'unreachable';

  panel.card.dataset.state = state;
  panel.card.dataset.health = health;
  panel.stateBadge.dataset.value = state;
  panel.stateBadge.textContent = state;
  panel.healthBadge.dataset.value = health;
  panel.healthBadge.textContent = health;

  if (!pending.has(panel.id)) {
    const nextSwitchState = switchState(subsystem.state);
    setSwitch(panel, nextSwitchState, switchLabel(subsystem.title, nextSwitchState));
  }

  renderParameters(panel, subsystem.parameters);
  panel.error.textContent = subsystem.error ?? '';
}

function renderUnavailable(panel: Panel, reason: string): void {
  panel.card.dataset.state = 'unreachable';
  panel.card.dataset.health = 'unreachable';
  panel.stateBadge.dataset.value = 'unreachable';
  panel.stateBadge.textContent = 'unreachable';
  panel.healthBadge.dataset.value = 'unreachable';
  panel.healthBadge.textContent = 'unreachable';

  if (!pending.has(panel.id)) {
    setSwitch(panel, 'unknown', switchLabel(panel.title, 'unknown'));
  }

  renderParameters(panel, []);
  panel.error.textContent = reason;
}

function renderParameters(panel: Panel, parameters: Parameter[]): void {
  panel.parameters.replaceChildren(
    ...parameters.map((parameter) => {
      const name = document.createElement('dt');
      name.textContent = parameter.name;
      const value = document.createElement('dd');
      value.textContent = parameter.value;
      const entry = document.createElement('div');
      entry.className = 'parameter';
      entry.append(name, value);
      return entry;
    }),
  );
}

async function refreshStatus(): Promise<void> {
  if (statusInFlight) {
    return;
  }
  statusInFlight = true;

  try {
    const response = await fetch('/api/subsystems');
    if (!response.ok) {
      throw new Error(`status request failed with HTTP ${response.status}`);
    }
    renderStatus((await response.json()) as StatusResponse);
  } catch (error) {
    for (const panel of panels) {
      renderUnavailable(panel, 'mixer console backend is unavailable');
    }
    showMessage(`Could not load subsystem status: ${errorMessage(error)}`, 'error');
  } finally {
    statusInFlight = false;
  }
}

async function toggle(panel: Panel): Promise<void> {
  const current = panel.switchButton.dataset.state;
  if (pending.has(panel.id) || (current !== 'on' && current !== 'off')) {
    return;
  }

  const action = current === 'on' ? 'stop' : 'start';
  pending.add(panel.id);
  clearMessage();
  setSwitch(
    panel,
    'busy',
    action === 'start' ? `Starting ${panel.id}...` : `Stopping ${panel.id}...`,
  );

  try {
    const response = await fetch(`/api/subsystems/${panel.id}/${action}`, { method: 'POST' });
    const body = (await response.json().catch(() => null)) as ActionResponse | null;
    const expected = action === 'start' ? 'started' : 'stopped';

    if (!response.ok || body === null || body.status !== expected) {
      throw new Error(formatActionError(action, response.status, body));
    }
  } catch (error) {
    showMessage(errorMessage(error), 'error');
  } finally {
    pending.delete(panel.id);
    await refreshStatus();
  }
}

function formatActionError(
  action: string,
  statusCode: number,
  body: ActionResponse | null,
): string {
  const direction = action === 'start' ? 'on' : 'off';
  const detail = body?.subsystem?.error;
  if (detail) {
    return `Could not turn ${body?.subsystem?.title ?? 'subsystem'} ${direction}: ${detail}`;
  }
  return `Could not turn subsystem ${direction} (HTTP ${statusCode})`;
}

for (const panel of panels) {
  panel.switchButton.addEventListener('click', () => {
    void toggle(panel);
  });
}

void refreshStatus();
window.setInterval(() => {
  void refreshStatus();
}, POLL_INTERVAL_MS);
