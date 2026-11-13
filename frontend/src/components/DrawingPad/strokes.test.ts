import { describe, expect, it } from 'vitest';
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

const black: PenColor = '#000';
const blue: PenColor = '#3b5998';
const red: PenColor = '#d93025';
const green: PenColor = '#188038';
const thin: BrushSize = 4;
const thick: BrushSize = 12;

function line(color: PenColor = black, size: BrushSize = thin, pressure?: number): Drawing {
  const points: PenPoint[] = [
    { x: 10, y: 10, pressure },
    { x: 40, y: 30, pressure },
    { x: 70, y: 20, pressure },
  ];
  return points.reduce((drawing, point) => addPoint(drawing, point, color, size), emptyDrawing());
}

class FakeContext {
  fillStyle = '';
  transforms: number[][] = [];
  clears: number[][] = [];
  rects: { style: string; x: number; y: number; w: number; h: number }[] = [];
  lines: number[][] = [];
  fillStyles: string[] = [];

  setTransform(a: number, b: number, c: number, d: number, e: number, f: number) {
    this.transforms.push([a, b, c, d, e, f]);
  }

  clearRect(x: number, y: number, w: number, h: number) {
    this.clears.push([x, y, w, h]);
  }

  fillRect(x: number, y: number, w: number, h: number) {
    this.rects.push({ style: this.fillStyle, x, y, w, h });
  }

  beginPath() {
    this.lines = [];
  }

  moveTo(x: number, y: number) {
    this.lines.push([x, y]);
  }

  lineTo(x: number, y: number) {
    this.lines.push([x, y]);
  }

  closePath() {}

  fill() {
    this.fillStyles.push(this.fillStyle);
  }
}

function draw(drawing: Drawing, ratio = 1): FakeContext {
  const ctx = new FakeContext();
  render(ctx as unknown as CanvasRenderingContext2D, drawing, ratio);
  return ctx;
}

describe('strokes', () => {
  it('uses the pad size, four pens and two brushes from the card', () => {
    expect(PAD_WIDTH).toBe(400);
    expect(PAD_HEIGHT).toBe(200);
    expect(PENS.map((pen) => pen.color)).toEqual([black, blue, red, green]);
    expect(BRUSHES.map((brush) => brush.size)).toEqual([thin, thick]);
  });

  it('adds points without mutating the previous drawing', () => {
    const start = emptyDrawing();
    const started = addPoint(start, { x: 1, y: 2 }, red, thick);
    const extended = addPoint(started, { x: 3, y: 4 }, blue, thin);

    expect(start).toEqual({ strokes: [], current: null });
    expect(started.current).toEqual({ points: [{ x: 1, y: 2 }], color: red, size: thick });
    expect(extended.current).toEqual({
      color: red,
      size: thick,
      points: [
        { x: 1, y: 2 },
        { x: 3, y: 4 },
      ],
    });
    expect(started.current?.points).toHaveLength(1);
  });

  it('ends a stroke, ignores an empty end, and reports empty', () => {
    expect(isEmpty(emptyDrawing())).toBe(true);
    expect(endStroke(emptyDrawing())).toEqual(emptyDrawing());

    const open = line();
    expect(isEmpty(open)).toBe(false);
    const ended = endStroke(open);
    expect(ended.current).toBeNull();
    expect(ended.strokes).toHaveLength(1);
    expect(ended.strokes[0]?.color).toBe(black);
    expect(isEmpty(ended)).toBe(false);
    expect(endStroke(ended)).toBe(ended);

    const blank = endStroke({ strokes: [], current: { points: [], color: black, size: thin } });
    expect(isEmpty(blank)).toBe(true);
  });

  it('undo removes the last finished stroke and clear drops everything', () => {
    const first = endStroke(line(black, thin));
    const second = endStroke(addPoint(first, { x: 5, y: 5 }, green, thick));
    const open = addPoint(second, { x: 8, y: 8 }, blue, thin);

    const undone = undo(open);
    expect(undone.strokes).toHaveLength(1);
    expect(undone.current?.color).toBe(blue);
    expect(undo(undone).strokes).toHaveLength(0);
    const empty = emptyDrawing();
    expect(undo(empty)).toBe(empty);

    const kept = endStroke(line(red, thick));
    expect(isEmpty(clear())).toBe(true);
    expect(kept.strokes).toHaveLength(1);
  });

  it('renders a white pad, smoothed outlines, and a thicker brush', () => {
    const empty = draw(emptyDrawing(), 2);
    expect(empty.clears).toEqual([[0, 0, 800, 400]]);
    expect(empty.transforms).toContainEqual([2, 0, 0, 2, 0, 0]);
    expect(empty.rects).toEqual([{ style: '#ffffff', x: 0, y: 0, w: 400, h: 200 }]);
    expect(empty.fillStyles).toEqual([]);

    const thinCtx = draw(endStroke(line(blue, thin)));
    const thickCtx = draw(endStroke(line(blue, thick)));
    expect(thinCtx.fillStyles).toEqual([blue]);
    expect(thinCtx.lines.length).toBeGreaterThan(10);
    expect(farthest(thickCtx.lines)).toBeGreaterThan(farthest(thinCtx.lines));

    const open = draw(line(red, thin));
    const closed = draw(endStroke(line(red, thin)));
    expect(open.fillStyles).toEqual([red]);
    expect(open.lines).not.toEqual(closed.lines);

    const pressured = draw(
      endStroke(
        [0.2, 0.8, 0.4].reduce(
          (drawing, pressure, index) => addPoint(drawing, { x: index * 20, y: 15, pressure }, green, thin),
          emptyDrawing(),
        ),
      ),
    );
    expect(pressured.fillStyles).toEqual([green]);
    expect(pressured.lines.length).toBeGreaterThan(3);

    const fallback = draw(emptyDrawing(), 0);
    expect(fallback.clears).toEqual([[0, 0, PAD_WIDTH, PAD_HEIGHT]]);
  });
});

function farthest(lines: number[][]): number {
  return lines.reduce((max, point) => {
    const x = point[0] ?? 0;
    const y = point[1] ?? 0;
    return Math.max(max, Math.hypot(x - 40, y - 30));
  }, 0);
}
