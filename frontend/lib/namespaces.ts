/** Path segment standing for "monitors without a namespace"; no Kubernetes name can be this. */
export const MANUAL_SLUG = "_";

export const MANUAL_LABEL = "Elle eklenenler";

export function namespaceHref(namespace: string): string {
  return `/namespaces/${namespace === "" ? MANUAL_SLUG : encodeURIComponent(namespace)}`;
}

/** The namespace a path segment refers to. */
export function namespaceFromSlug(slug: string): string {
  const name = decodeURIComponent(slug);
  return name === MANUAL_SLUG ? "" : name;
}

/** Namespaces alphabetically, monitors without one last. */
export function compareNamespaces(a: string, b: string): number {
  if (a === "") return 1;
  if (b === "") return -1;
  return a.localeCompare(b);
}
