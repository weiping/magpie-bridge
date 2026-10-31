/** SP13-style routing gate + model id helpers. */

export function stripModelSuffix(id: string): string {
  return id.replace(/\[[^\]]*\]$/, "");
}

export function isRoutingModel(
  providerId: string | undefined,
  modelId: string | undefined,
  magpieProvider: string,
  routingModelId: string,
): boolean {
  if (providerId !== magpieProvider || !modelId) return false;
  return stripModelSuffix(modelId) === stripModelSuffix(routingModelId);
}

/** mbridge returns "group/mb-fast"; OpenClaw model id is the same without provider prefix. */
export function groupToModelId(group: string): string {
  return group.startsWith("group/") ? group : `group/${group}`;
}

export function modelKey(provider: string, modelId: string): string {
  return `${provider}/${modelId}`;
}
