const DECRYPTED_REPOS = new Set<string>();
const STORAGE_PREFIX = 'encrypted_repo_';

export function isRepoDecrypted(repoId: string): boolean {
  return DECRYPTED_REPOS.has(repoId);
}

export function markRepoDecrypted(repoId: string): void {
  DECRYPTED_REPOS.add(repoId);
}

export function getRememberedPassword(repoId: string): string | null {
  return localStorage.getItem(`${STORAGE_PREFIX}${repoId}`);
}

export function setRememberedPassword(repoId: string, password: string): void {
  localStorage.setItem(`${STORAGE_PREFIX}${repoId}`, password);
}

export function clearRememberedPassword(repoId: string): void {
  localStorage.removeItem(`${STORAGE_PREFIX}${repoId}`);
}
