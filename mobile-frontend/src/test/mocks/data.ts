import type { Dirent, Repo, Activity, SearchResult } from '../../lib/models';
import type { StarredFile, ShareLink, Group, GroupMember, GroupRepo, ShareItem, SearchedUser } from '../../lib/api';

export const MOCK_USER = {
  name: 'Test User',
  email: 'test@example.com',
  avatarURL: '/default-avatar.png',
};

export const MOCK_REPOS: Repo[] = [
  {
    repo_id: 'repo-001',
    repo_name: 'My Documents',
    size: 104857600,
    permission: 'rw',
    owner_email: 'test@example.com',
    owner_name: 'Test User',
    encrypted: false,
    last_modified: '2026-03-01T10:00:00Z',
  },
  {
    repo_id: 'repo-002',
    repo_name: 'Photos',
    size: 524288000,
    permission: 'rw',
    owner_email: 'test@example.com',
    owner_name: 'Test User',
    encrypted: false,
    last_modified: '2026-03-05T14:30:00Z',
  },
  {
    repo_id: 'repo-003',
    repo_name: 'Shared Project',
    size: 209715200,
    permission: 'r',
    owner_email: 'alice@example.com',
    owner_name: 'Alice Smith',
    encrypted: false,
    last_modified: '2026-03-08T09:15:00Z',
  },
  {
    repo_id: 'repo-004',
    repo_name: 'Encrypted Vault',
    size: 52428800,
    permission: 'rw',
    owner_email: 'test@example.com',
    owner_name: 'Test User',
    encrypted: true,
    last_modified: '2026-02-20T16:45:00Z',
  },
  {
    repo_id: 'repo-005',
    repo_name: 'Team Files',
    size: 1073741824,
    permission: 'rw',
    owner_email: 'bob@example.com',
    owner_name: 'Bob Jones',
    encrypted: false,
    last_modified: '2026-03-10T11:00:00Z',
  },
];

export const MOCK_DIRENTS: Dirent[] = [
  { id: 'dir-001', type: 'dir', name: 'Reports', size: 0, mtime: 1709280000, permission: 'rw' },
  { id: 'dir-002', type: 'dir', name: 'Images', size: 0, mtime: 1709366400, permission: 'rw' },
  { id: 'dir-003', type: 'dir', name: 'Archive', size: 0, mtime: 1708675200, permission: 'r' },
  { id: 'file-001', type: 'file', name: 'document.pdf', size: 1048576, mtime: 1709452800, permission: 'rw', starred: true },
  { id: 'file-002', type: 'file', name: 'photo.jpg', size: 2097152, mtime: 1709539200, permission: 'rw' },
  { id: 'file-003', type: 'file', name: 'spreadsheet.xlsx', size: 524288, mtime: 1709625600, permission: 'rw' },
  { id: 'file-004', type: 'file', name: 'notes.txt', size: 4096, mtime: 1709712000, permission: 'rw', modifier_email: 'alice@example.com', modifier_name: 'Alice' },
  { id: 'file-005', type: 'file', name: 'video.mp4', size: 52428800, mtime: 1709798400, permission: 'rw' },
  { id: 'file-006', type: 'file', name: 'presentation.pptx', size: 10485760, mtime: 1709884800, permission: 'r' },
  { id: 'file-007', type: 'file', name: 'code.py', size: 8192, mtime: 1709971200, permission: 'rw', starred: true },
];

export const MOCK_STARRED: StarredFile[] = [
  { repo_id: 'repo-001', repo_name: 'My Documents', path: '/document.pdf', obj_name: 'document.pdf', mtime: 1709452800, size: 1048576, is_dir: false },
  { repo_id: 'repo-001', repo_name: 'My Documents', path: '/code.py', obj_name: 'code.py', mtime: 1709971200, size: 8192, is_dir: false },
  { repo_id: 'repo-002', repo_name: 'Photos', path: '/vacation', obj_name: 'vacation', mtime: 1709366400, size: 0, is_dir: true },
];

export const MOCK_GROUPS: Group[] = [
  { id: 1, name: 'Engineering', owner: 'test@example.com', created_at: '2026-01-15T10:00:00Z', member_count: 8 },
  { id: 2, name: 'Design Team', owner: 'alice@example.com', created_at: '2026-02-01T14:00:00Z', member_count: 5 },
  { id: 3, name: 'Marketing', owner: 'bob@example.com', created_at: '2026-02-20T09:00:00Z', member_count: 12 },
];

export const MOCK_GROUP_MEMBERS: GroupMember[] = [
  { email: 'test@example.com', name: 'Test User', role: 'Owner', avatar_url: '/avatar1.png' },
  { email: 'alice@example.com', name: 'Alice Smith', role: 'Admin', avatar_url: '/avatar2.png' },
  { email: 'bob@example.com', name: 'Bob Jones', role: 'Member', avatar_url: '/avatar3.png' },
];

export const MOCK_GROUP_REPOS: GroupRepo[] = [
  {
    repo_id: 'repo-005', repo_name: 'Team Files', permission: 'rw', size: 1073741824,
    owner_email: 'bob@example.com', owner_name: 'Bob Jones', encrypted: false,
    last_modified: '2026-03-10T11:00:00Z', modifier_email: 'test@example.com', modifier_name: 'Test User',
  },
];

export const MOCK_ACTIVITIES: Activity[] = [
  { op_type: 'create', repo_id: 'repo-001', repo_name: 'My Documents', obj_type: 'file', commit_id: 'c001', name: 'report.pdf', path: '/report.pdf', author_email: 'test@example.com', author_name: 'Test User', author_contact_email: 'test@example.com', avatar_url: '/avatar1.png', time: '2026-03-10T10:00:00Z' },
  { op_type: 'edit', repo_id: 'repo-001', repo_name: 'My Documents', obj_type: 'file', commit_id: 'c002', name: 'notes.txt', path: '/notes.txt', author_email: 'alice@example.com', author_name: 'Alice Smith', author_contact_email: 'alice@example.com', avatar_url: '/avatar2.png', time: '2026-03-10T09:30:00Z' },
  { op_type: 'delete', repo_id: 'repo-002', repo_name: 'Photos', obj_type: 'file', commit_id: 'c003', name: 'old-photo.jpg', path: '/old-photo.jpg', author_email: 'test@example.com', author_name: 'Test User', author_contact_email: 'test@example.com', avatar_url: '/avatar1.png', time: '2026-03-09T16:00:00Z' },
  { op_type: 'rename', repo_id: 'repo-001', repo_name: 'My Documents', obj_type: 'file', commit_id: 'c004', name: 'final-report.pdf', path: '/final-report.pdf', old_name: 'draft.pdf', author_email: 'test@example.com', author_name: 'Test User', author_contact_email: 'test@example.com', avatar_url: '/avatar1.png', time: '2026-03-09T14:00:00Z' },
  { op_type: 'move', repo_id: 'repo-003', repo_name: 'Shared Project', obj_type: 'dir', commit_id: 'c005', name: 'archive', path: '/old/archive', author_email: 'bob@example.com', author_name: 'Bob Jones', author_contact_email: 'bob@example.com', avatar_url: '/avatar3.png', time: '2026-03-09T12:00:00Z' },
  { op_type: 'create', repo_id: 'repo-005', repo_name: 'Team Files', obj_type: 'file', commit_id: 'c006', name: 'design.fig', path: '/design.fig', author_email: 'alice@example.com', author_name: 'Alice Smith', author_contact_email: 'alice@example.com', avatar_url: '/avatar2.png', time: '2026-03-08T18:00:00Z' },
  { op_type: 'edit', repo_id: 'repo-001', repo_name: 'My Documents', obj_type: 'file', commit_id: 'c007', name: 'spreadsheet.xlsx', path: '/spreadsheet.xlsx', author_email: 'test@example.com', author_name: 'Test User', author_contact_email: 'test@example.com', avatar_url: '/avatar1.png', time: '2026-03-08T15:00:00Z' },
  { op_type: 'create', repo_id: 'repo-002', repo_name: 'Photos', obj_type: 'dir', commit_id: 'c008', name: 'vacation-2026', path: '/vacation-2026', author_email: 'test@example.com', author_name: 'Test User', author_contact_email: 'test@example.com', avatar_url: '/avatar1.png', time: '2026-03-07T10:00:00Z' },
  { op_type: 'delete', repo_id: 'repo-003', repo_name: 'Shared Project', obj_type: 'file', commit_id: 'c009', name: 'temp.log', path: '/temp.log', author_email: 'bob@example.com', author_name: 'Bob Jones', author_contact_email: 'bob@example.com', avatar_url: '/avatar3.png', time: '2026-03-06T08:00:00Z' },
  { op_type: 'edit', repo_id: 'repo-005', repo_name: 'Team Files', obj_type: 'file', commit_id: 'c010', name: 'README.md', path: '/README.md', author_email: 'alice@example.com', author_name: 'Alice Smith', author_contact_email: 'alice@example.com', avatar_url: '/avatar2.png', time: '2026-03-05T12:00:00Z' },
];

export const MOCK_SHARE_LINKS: ShareLink[] = [
  {
    token: 'abc123token',
    link: 'http://localhost:8080/d/abc123token/',
    repo_id: 'repo-001',
    path: '/document.pdf',
    is_dir: false,
    is_expired: false,
    expire_date: '2026-04-10T00:00:00Z',
    permissions: { can_edit: false, can_download: true },
    ctime: '2026-03-10T10:00:00Z',
    view_cnt: 5,
  },
  {
    token: 'def456token',
    link: 'http://localhost:8080/d/def456token/',
    repo_id: 'repo-001',
    path: '/Reports',
    is_dir: true,
    is_expired: false,
    expire_date: null,
    permissions: { can_edit: true, can_download: true },
    ctime: '2026-03-08T14:00:00Z',
    view_cnt: 12,
  },
];

export const MOCK_SHARE_ITEMS: ShareItem[] = [
  { user_email: 'alice@example.com', user_name: 'Alice Smith', avatar_url: '/avatar2.png', permission: 'rw' },
  { user_email: 'bob@example.com', user_name: 'Bob Jones', avatar_url: '/avatar3.png', permission: 'r' },
];

export const MOCK_SEARCHED_USERS: SearchedUser[] = [
  { email: 'alice@example.com', name: 'Alice Smith', avatar_url: '/avatar2.png' },
  { email: 'bob@example.com', name: 'Bob Jones', avatar_url: '/avatar3.png' },
];

export const MOCK_SEARCH_RESULTS: SearchResult[] = [
  { repo_id: 'repo-001', repo_name: 'My Documents', name: 'report.pdf', path: '/report.pdf', size: 1048576, mtime: 1709452800, is_dir: false },
  { repo_id: 'repo-001', repo_name: 'My Documents', name: 'notes.txt', path: '/notes.txt', size: 4096, mtime: 1709712000, is_dir: false },
  { repo_id: 'repo-003', repo_name: 'Shared Project', name: 'data', path: '/data', size: 0, mtime: 1709280000, is_dir: true },
];
