export interface Dirent {
  id: string;
  type: 'file' | 'dir';
  name: string;
  size: number;
  mtime: number;
  permission: string;
  starred?: boolean;
  modifier_email?: string;
  modifier_name?: string;
}

export interface Repo {
  repo_id: string;
  repo_name: string;
  size: number;
  permission: string;
  owner_email: string;
  owner_name: string;
  encrypted: boolean;
  last_modified: string;
}

export function bytesToSize(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export function formatDate(timestamp: number): string {
  return new Date(timestamp * 1000).toLocaleString();
}
