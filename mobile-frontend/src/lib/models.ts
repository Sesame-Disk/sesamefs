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

export interface SearchResult {
  repo_id: string;
  repo_name: string;
  name: string;
  path: string;
  size: number;
  mtime: number;
  is_dir: boolean;
}

export interface Activity {
  op_type: 'create' | 'delete' | 'edit' | 'rename' | 'move';
  repo_id: string;
  repo_name: string;
  obj_type: string;
  commit_id: string;
  name: string;
  path: string;
  old_name?: string;
  author_email: string;
  author_name: string;
  author_contact_email: string;
  avatar_url: string;
  time: string;
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
