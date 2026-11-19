export function profileName(name: string, nickname: string): string {
  if (!nickname) return name;
  const parts = name.trim().split(/\s+/);
  parts.splice(Math.max(1, parts.length - 1), 0, `"${nickname}"`);
  return parts.join(' ').trim();
}
