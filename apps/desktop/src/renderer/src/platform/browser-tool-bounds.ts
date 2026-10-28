interface Bounds { x: number; y: number; width: number; height: number }

/** Native guest views paint above DOM. Reserve the chat's right-hand column
 * instead of hiding the whole website while the user talks to an agent. */
export function browserBoundsBesideChat(surface: Bounds, chat: Bounds | null): Bounds | null {
  if (!chat || chat.x >= surface.x + surface.width || chat.x + chat.width <= surface.x || chat.y >= surface.y + surface.height || chat.y + chat.height <= surface.y) return surface;
  const width = Math.min(surface.width, chat.x - surface.x - 8);
  return width >= 160 ? { ...surface, width } : null;
}
