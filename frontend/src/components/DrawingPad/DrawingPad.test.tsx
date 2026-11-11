import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { setupVitestCanvasMock } from 'vitest-canvas-mock';
import { describe, expect, it, vi } from 'vitest';
import { render } from '../../test/render';
import { DrawingPad } from './index';

// The package marks itself side-effect free, so the canvas patch has to be called.
setupVitestCanvasMock();

function pad(): HTMLElement {
  return screen.getByRole('application', { name: 'Drawing pad' });
}

function draw(points: Array<[number, number]>, pointerType: 'mouse' | 'touch' = 'mouse') {
  const canvas = pad();
  const first = points[0];
  const last = points[points.length - 1];
  if (!first || !last) throw new Error('a stroke needs a point');
  const pressure = pointerType === 'touch' ? 0.7 : 0.5;
  fireEvent.pointerDown(canvas, {
    clientX: first[0],
    clientY: first[1],
    pointerId: 1,
    pointerType,
    button: 0,
    buttons: 1,
    pressure,
  });
  for (const point of points.slice(1)) {
    fireEvent.pointerMove(canvas, {
      clientX: point[0],
      clientY: point[1],
      pointerId: 1,
      pointerType,
      button: 0,
      buttons: 1,
      pressure,
    });
  }
  fireEvent.pointerUp(canvas, {
    clientX: last[0],
    clientY: last[1],
    pointerId: 1,
    pointerType,
    button: 0,
    buttons: 0,
    pressure: 0,
  });
}

async function expectPng(onChange: ReturnType<typeof vi.fn>) {
  await waitFor(() => expect(onChange).toHaveBeenCalled());
  const blob = onChange.mock.lastCall?.[0] as Blob | null;
  expect(blob).toBeInstanceOf(Blob);
  expect(blob?.type).toBe('image/png');
  expect(blob && blob.size).toBeGreaterThan(0);
  return blob;
}

describe('DrawingPad', () => {
  it('shows a 400×200 pad, four colors, two brushes, undo and clear', () => {
    render(<DrawingPad onChange={vi.fn()} />);
    const canvas = pad();
    expect(canvas).toHaveStyle({ width: '400px', height: '200px', touchAction: 'none' });
    expect(canvas).toHaveAttribute('width', '400');
    expect(canvas).toHaveAttribute('height', '200');
    expect(screen.getByRole('toolbar', { name: 'Drawing tools' })).toBeInTheDocument();
    for (const name of ['Black', 'Blue', 'Red', 'Green', 'Thin brush', 'Thick brush']) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument();
    }
    expect(screen.getByRole('button', { name: 'Black' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Thin brush' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Clear' })).toBeDisabled();
  });

  it('sizes the bitmap for devicePixelRatio', () => {
    const original = Object.getOwnPropertyDescriptor(window, 'devicePixelRatio');
    Object.defineProperty(window, 'devicePixelRatio', { configurable: true, value: 2 });
    try {
      render(<DrawingPad onChange={vi.fn()} />);
      const canvas = pad() as HTMLCanvasElement;
      expect(canvas.width).toBe(800);
      expect(canvas.height).toBe(400);
      expect(canvas).toHaveStyle({ width: '400px', height: '200px' });
    } finally {
      if (original) Object.defineProperty(window, 'devicePixelRatio', original);
      else Reflect.deleteProperty(window, 'devicePixelRatio');
    }
  });

  it('emits a png blob after a pointer stroke and not before the pointer is up', async () => {
    const onChange = vi.fn();
    render(<DrawingPad onChange={onChange} />);
    const canvas = pad();
    fireEvent.pointerDown(canvas, { clientX: 12, clientY: 12, pointerId: 1, pointerType: 'mouse', button: 0, buttons: 1 });
    fireEvent.pointerMove(canvas, { clientX: 48, clientY: 30, pointerId: 1, pointerType: 'mouse', button: 0, buttons: 1 });
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.pointerUp(canvas, { clientX: 48, clientY: 30, pointerId: 1, pointerType: 'mouse', button: 0, buttons: 0 });

    const blob = await expectPng(onChange);
    expect(blob).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Undo' })).toBeEnabled();

    const ctx = (canvas as HTMLCanvasElement).getContext('2d');
    expect(ctx).not.toBeNull();
    const fills = ctx?.__getDrawCalls().filter((call) => call.type === 'fill') ?? [];
    const rects = ctx?.__getDrawCalls().filter((call) => call.type === 'fillRect') ?? [];
    expect(fills.length).toBeGreaterThan(0);
    expect(rects.some((call) => call.props.width === 400 && call.props.height === 200)).toBe(true);
    const styles = ctx?.__getEvents().filter((event) => event.type === 'fillStyle').map((event) => String(event.props.value).toLowerCase()) ?? [];
    expect(styles).toContain('#ffffff');
  });

  it('emits a blob for a touch stroke in the selected color', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<DrawingPad onChange={onChange} />);
    await user.click(screen.getByRole('button', { name: 'Red' }));
    await user.click(screen.getByRole('button', { name: 'Thick brush' }));
    expect(screen.getByRole('button', { name: 'Red' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Black' })).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('button', { name: 'Thick brush' })).toHaveAttribute('aria-pressed', 'true');

    draw(
      [
        [16, 20],
        [60, 40],
        [90, 18],
      ],
      'touch',
    );
    await expectPng(onChange);
    const ctx = (pad() as HTMLCanvasElement).getContext('2d');
    const styles = ctx?.__getEvents().filter((event) => event.type === 'fillStyle').map((event) => String(event.props.value).toLowerCase()) ?? [];
    expect(styles).toContain('#d93025');
  });

  it('emits null when undo or clear leaves the pad empty', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<DrawingPad onChange={onChange} />);
    draw([
      [10, 10],
      [30, 20],
    ]);
    await expectPng(onChange);

    await user.click(screen.getByRole('button', { name: 'Undo' }));
    expect(onChange).toHaveBeenLastCalledWith(null);
    expect(screen.getByRole('button', { name: 'Undo' })).toBeDisabled();

    draw([
      [20, 20],
      [70, 50],
      [100, 30],
    ]);
    draw([
      [15, 40],
      [55, 60],
    ]);
    await waitFor(() => expect(onChange.mock.calls.filter(([blob]) => blob instanceof Blob).length).toBeGreaterThanOrEqual(2));
    await user.click(screen.getByRole('button', { name: 'Undo' }));
    await waitFor(() => {
      const last = onChange.mock.lastCall?.[0] as Blob | null;
      expect(last).toBeInstanceOf(Blob);
    });
    await user.click(screen.getByRole('button', { name: 'Clear' }));
    expect(onChange).toHaveBeenLastCalledWith(null);
    expect(screen.getByRole('button', { name: 'Clear' })).toBeDisabled();
  });

  it('ignores a stale blob that resolves after clear', async () => {
    const onChange = vi.fn();
    render(<DrawingPad onChange={onChange} />);
    draw([
      [8, 8],
      [40, 24],
    ]);
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
    await waitFor(() => expect(onChange).toHaveBeenCalledWith(null));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(onChange).toHaveBeenLastCalledWith(null);
  });

  it('captures the pointer and still draws if capture throws', async () => {
    const onChange = vi.fn();
    render(<DrawingPad onChange={onChange} />);
    const canvas = pad() as HTMLCanvasElement;
    const release = vi.fn();
    canvas.setPointerCapture = vi.fn();
    canvas.hasPointerCapture = () => true;
    canvas.releasePointerCapture = release;

    fireEvent.pointerDown(canvas, { clientX: 12, clientY: 14, pointerId: 7, pointerType: 'mouse', button: 0, buttons: 1, pressure: 0.5 });
    expect(canvas.setPointerCapture).toHaveBeenCalledWith(7);
    fireEvent.pointerUp(canvas, { clientX: 20, clientY: 18, pointerId: 7, pointerType: 'mouse', button: 0, buttons: 0 });
    expect(release).toHaveBeenCalledWith(7);
    await expectPng(onChange);

    onChange.mockClear();
    canvas.setPointerCapture = () => {
      throw new Error('pointer gone');
    };
    fireEvent.pointerDown(canvas, { clientX: 14, clientY: 16, pointerId: 8, pointerType: 'mouse', button: 0, buttons: 1, pressure: 0.5 });
    fireEvent.pointerUp(canvas, { clientX: 22, clientY: 19, pointerId: 8, pointerType: 'mouse', button: 0, buttons: 0 });
    await expectPng(onChange);
  });

  it('ignores a right click, a move with no stroke, and ends on pointer cancel', async () => {
    const onChange = vi.fn();
    render(<DrawingPad onChange={onChange} />);
    const canvas = pad();
    fireEvent.pointerMove(canvas, { clientX: 10, clientY: 10, pointerId: 3, pointerType: 'mouse', buttons: 1 });
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 10, pointerId: 4, pointerType: 'mouse', button: 2, buttons: 2 });
    fireEvent.pointerUp(canvas, { clientX: 10, clientY: 10, pointerId: 4, pointerType: 'mouse', button: 2, buttons: 0 });
    expect(onChange).not.toHaveBeenCalled();

    fireEvent.pointerDown(canvas, { clientX: 12, clientY: 14, pointerId: 5, pointerType: 'pen', button: 0, buttons: 1, pressure: 0.4 });
    fireEvent.pointerDown(canvas, { clientX: 20, clientY: 20, pointerId: 6, pointerType: 'pen', button: 0, buttons: 1, pressure: 0.4 });
    fireEvent.pointerCancel(canvas, { clientX: 18, clientY: 16, pointerId: 5, pointerType: 'pen', button: 0, buttons: 0, pressure: 0.4 });
    await expectPng(onChange);
  });
});
