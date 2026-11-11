import { getStroke } from 'perfect-freehand';

/** CSS pixel size of the wall drawing pad. The bitmap is this times devicePixelRatio. */
export const PAD_WIDTH = 400;
export const PAD_HEIGHT = 200;

export const PENS = [
  { color: '#000', label: 'Black' },
  { color: '#3b5998', label: 'Blue' },
  { color: '#d93025', label: 'Red' },
  { color: '#188038', label: 'Green' },
] as const;

/** Diameters in CSS pixels. The card asks for two sizes and does not pick the values. */
export const BRUSHES = [
  { size: 4, label: 'Thin brush', short: 'Thin' },
  { size: 12, label: 'Thick brush', short: 'Thick' },
] as const;

export type PenColor = (typeof PENS)[number]['color'];
export type BrushSize = (typeof BRUSHES)[number]['size'];

export type PenPoint = {
  x: number;
  y: number;
  /** 0–1 when the device reports pressure. Omitted for a mouse (browsers send 0.5). */
  pressure?: number;
};

export type Stroke = {
  points: PenPoint[];
  color: PenColor;
  size: BrushSize;
};

export type Drawing = {
  strokes: Stroke[];
  current: Stroke | null;
};

export function emptyDrawing(): Drawing {
  return { strokes: [], current: null };
}

export function isEmpty(drawing: Drawing): boolean {
  const current = drawing.current;
  return drawing.strokes.length === 0 && (current === null || current.points.length === 0);
}

/** Starts a stroke, or appends to the open one. Color and size stick to the first point. */
export function addPoint(drawing: Drawing, point: PenPoint, color: PenColor, size: BrushSize): Drawing {
  const current = drawing.current;
  if (current === null || current.points.length === 0) {
    return { strokes: drawing.strokes, current: { points: [point], color, size } };
  }
  return {
    strokes: drawing.strokes,
    current: { color: current.color, size: current.size, points: [...current.points, point] },
  };
}

export function endStroke(drawing: Drawing): Drawing {
  const current = drawing.current;
  if (current === null) return drawing;
  if (current.points.length === 0) return { strokes: drawing.strokes, current: null };
  return { strokes: [...drawing.strokes, current], current: null };
}

/** Drops the last finished stroke. An in-progress stroke is left alone. */
export function undo(drawing: Drawing): Drawing {
  if (drawing.strokes.length === 0) return drawing;
  return { strokes: drawing.strokes.slice(0, -1), current: drawing.current };
}

export function clear(): Drawing {
  return emptyDrawing();
}

function usesDevicePressure(points: PenPoint[]): boolean {
  return points.some((point) => point.pressure !== undefined && point.pressure !== 0.5);
}

function paintStroke(ctx: CanvasRenderingContext2D, stroke: Stroke, done: boolean): void {
  const devicePressure = usesDevicePressure(stroke.points);
  const input = devicePressure
    ? stroke.points.map((point) => [point.x, point.y, point.pressure ?? 0.5])
    : stroke.points.map((point) => [point.x, point.y]);
  const outline = getStroke(input, {
    size: stroke.size,
    thinning: 0.5,
    smoothing: 0.5,
    streamline: 0.5,
    simulatePressure: !devicePressure,
    last: done,
  });
  const start = outline[0];
  if (start === undefined) return;
  ctx.fillStyle = stroke.color;
  ctx.beginPath();
  ctx.moveTo(start[0], start[1]);
  for (let index = 1; index < outline.length; index += 1) {
    const point = outline[index];
    if (point === undefined) continue;
    ctx.lineTo(point[0], point[1]);
  }
  ctx.closePath();
  ctx.fill();
}

/** Draws a white pad and every stroke in CSS-pixel space, scaled by devicePixelRatio. */
export function render(ctx: CanvasRenderingContext2D, drawing: Drawing, devicePixelRatio = 1): void {
  const ratio = devicePixelRatio > 0 ? devicePixelRatio : 1;
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, PAD_WIDTH * ratio, PAD_HEIGHT * ratio);
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, PAD_WIDTH, PAD_HEIGHT);
  for (const stroke of drawing.strokes) paintStroke(ctx, stroke, true);
  const current = drawing.current;
  if (current !== null && current.points.length > 0) paintStroke(ctx, current, false);
}
