import { Button, Group, Stack, UnstyledButton } from '@mantine/core';
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent } from 'react';
import { BORDER } from '../../theme/theme';
import {
  BRUSHES,
  PAD_HEIGHT,
  PAD_WIDTH,
  PENS,
  addPoint,
  clear,
  emptyDrawing,
  endStroke,
  isEmpty,
  render,
  undo,
  type BrushSize,
  type Drawing,
  type PenColor,
  type PenPoint,
} from './strokes';

export type DrawingPadProps = {
  onChange: (blob: Blob | null) => void;
};

const defaultPen = PENS[0];
const defaultBrush = BRUSHES[0];

function bitmapRatio(): number {
  const ratio = window.devicePixelRatio;
  return ratio > 0 ? ratio : 1;
}

function clamp(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) return min;
  if (value < min) return min;
  if (value > max) return max;
  return value;
}

function pointFrom(event: PointerEvent<HTMLCanvasElement>): PenPoint {
  const rect = event.currentTarget.getBoundingClientRect();
  const width = rect.width || PAD_WIDTH;
  const height = rect.height || PAD_HEIGHT;
  const x = clamp(((event.clientX - rect.left) / width) * PAD_WIDTH, 0, PAD_WIDTH);
  const y = clamp(((event.clientY - rect.top) / height) * PAD_HEIGHT, 0, PAD_HEIGHT);
  // A mouse reports 0.5 while a button is held, and 0 otherwise. Real pens differ from 0.5.
  if (event.pressure > 0 && event.pressure !== 0.5) return { x, y, pressure: event.pressure };
  return { x, y };
}

export function DrawingPad({ onChange }: DrawingPadProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const drawingRef = useRef<Drawing>(emptyDrawing());
  const onChangeRef = useRef(onChange);
  const generation = useRef(0);
  const activePointer = useRef<number | null>(null);
  const [drawing, setDrawing] = useState<Drawing>(emptyDrawing);
  const [color, setColor] = useState<PenColor>(defaultPen.color);
  const [size, setSize] = useState<BrushSize>(defaultBrush.size);

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);
  const hasInk = drawing.strokes.length > 0;

  const paint = useCallback((next: Drawing) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    const ratio = canvas.width / PAD_WIDTH;
    render(ctx, next, ratio > 0 ? ratio : 1);
  }, []);

  const emit = useCallback((next: Drawing) => {
    const token = ++generation.current;
    if (isEmpty(next)) {
      onChangeRef.current(null);
      return;
    }
    const canvas = canvasRef.current;
    if (!canvas) return;
    canvas.toBlob((blob) => {
      if (token !== generation.current) return;
      onChangeRef.current(blob);
    }, 'image/png');
  }, []);

  useLayoutEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ratio = bitmapRatio();
    canvas.width = Math.round(PAD_WIDTH * ratio);
    canvas.height = Math.round(PAD_HEIGHT * ratio);
    paint(drawingRef.current);
  }, [paint]);

  function commit(next: Drawing, notify: boolean) {
    drawingRef.current = next;
    setDrawing(next);
    paint(next);
    if (notify) emit(next);
  }

  function onPointerDown(event: PointerEvent<HTMLCanvasElement>) {
    if (event.button !== 0) return;
    if (activePointer.current !== null) return;
    event.preventDefault();
    activePointer.current = event.pointerId;
    const target = event.currentTarget;
    if (typeof target.setPointerCapture === 'function') {
      try {
        target.setPointerCapture(event.pointerId);
      } catch {
        // The pointer can already be gone in tests and on some browsers.
      }
    }
    const next = addPoint(drawingRef.current, pointFrom(event), color, size);
    drawingRef.current = next;
    paint(next);
  }

  function onPointerMove(event: PointerEvent<HTMLCanvasElement>) {
    if (activePointer.current !== event.pointerId) return;
    const next = addPoint(drawingRef.current, pointFrom(event), color, size);
    drawingRef.current = next;
    paint(next);
  }

  function finish(event: PointerEvent<HTMLCanvasElement>) {
    if (activePointer.current !== event.pointerId) return;
    activePointer.current = null;
    const target = event.currentTarget;
    if (typeof target.hasPointerCapture === 'function' && target.hasPointerCapture(event.pointerId)) {
      target.releasePointerCapture(event.pointerId);
    }
    commit(endStroke(drawingRef.current), true);
  }

  function onUndo() {
    commit(undo(drawingRef.current), true);
  }

  function onClear() {
    activePointer.current = null;
    commit(clear(), true);
  }

  return (
    <Stack gap={6} align="flex-start">
      <Group gap={6} role="toolbar" aria-label="Drawing tools" wrap="wrap">
        {PENS.map((pen) => (
          <UnstyledButton
            key={pen.color}
            type="button"
            aria-label={pen.label}
            aria-pressed={color === pen.color}
            onClick={() => setColor(pen.color)}
            style={{
              width: 18,
              height: 18,
              padding: 0,
              background: pen.color,
              border: `1px solid ${color === pen.color ? '#333333' : BORDER}`,
            }}
          />
        ))}
        {BRUSHES.map((brush) => (
          <UnstyledButton
            key={brush.size}
            type="button"
            aria-label={brush.label}
            aria-pressed={size === brush.size}
            onClick={() => setSize(brush.size)}
            fz={12}
            px={6}
            style={{
              fontWeight: size === brush.size ? 700 : 400,
              border: `1px solid ${size === brush.size ? '#333333' : BORDER}`,
              background: '#ffffff',
            }}
          >
            {brush.short}
          </UnstyledButton>
        ))}
        <Button type="button" variant="default" size="xs" onClick={onUndo} disabled={!hasInk}>
          Undo
        </Button>
        <Button type="button" variant="default" size="xs" onClick={onClear} disabled={!hasInk}>
          Clear
        </Button>
      </Group>
      <canvas
        ref={canvasRef}
        role="application"
        aria-label="Drawing pad"
        width={PAD_WIDTH}
        height={PAD_HEIGHT}
        className="arc-drawing-pad"
        style={{
          width: PAD_WIDTH,
          height: PAD_HEIGHT,
          touchAction: 'none',
          backgroundColor: '#ffffff',
          cursor: 'crosshair',
          display: 'block',
        }}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={finish}
        onPointerCancel={finish}
      />
    </Stack>
  );
}
