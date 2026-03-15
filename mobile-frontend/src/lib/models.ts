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
  is_locked?: boolean;
  lock_owner?: string;
  lock_owner_name?: string;
  locked_by_me?: boolean;
  lock_time?: number;
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

export interface TrashItem {
  obj_name: string;
  obj_id: string;
  parent_dir: string;
  size: number;
  deleted_time: string;
  commit_id: string;
  is_dir: boolean;
}

export interface FileHistoryRecord {
  commit_id: string;
  rev_file_id: string;
  ctime: number;
  description: string;
  creator_name: string;
  creator_email: string;
  creator_contact_email: string;
  creator_avatar_url: string;
  path: string;
  rev_file_size: number;
  rev_renamed_old_path?: string;
  size?: number;
}

export interface RepoTag {
  id: number;
  repo_id: string;
  name: string;
  color: string;
  tag_id: number;
}

export interface FileTag {
  id: number;
  repo_tag_id: number;
  name: string;
  color: string;
  file_tag_id: number;
}

export interface Notification {
  id: number;
  type: string;
  detail: Record<string, unknown>;
  seen: boolean;
  timestamp: string;
  msg_from: string;
}

export interface LinkedDevice {
  device_id: string;
  device_name: string;
  platform: string;
  last_login_ip: string;
  last_accessed: string;
  is_desktop_client: boolean;
  client_version: string;
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
