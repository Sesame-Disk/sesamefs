import Dirent from '../dirent';

// Mock moment to avoid timezone issues in tests
jest.mock('moment', () => {
  const mockMoment = () => ({
    fromNow: () => 'a few seconds ago'
  });
  mockMoment.unix = (ts) => ({
    fromNow: () => 'a few seconds ago'
  });
  return mockMoment;
});

describe('Dirent model', () => {
  describe('constructor', () => {
    test('creates file dirent with all properties', () => {
      const json = {
        id: 'abc123',
        name: 'test.txt',
        type: 'file',
        mtime: 1234567890,
        size: 1024,
        permission: 'rw',
        starred: true,
        is_locked: true,
        lock_time: 1234567800,
        lock_owner: 'user@example.com',
        lock_owner_name: 'Test User',
        locked_by_me: false,
        modifier_name: 'Editor',
        modifier_email: 'editor@example.com',
        modifier_contact_email: 'editor@example.com',
        file_tags: [
          { file_tag_id: '1', repo_tag_id: '1', tag_name: 'Important', tag_color: 'red' }
        ],
        encoded_thumbnail_src: '/thumbnail/abc123'
      };

      const dirent = new Dirent(json);

      expect(dirent.id).toBe('abc123');
      expect(dirent.name).toBe('test.txt');
      expect(dirent.type).toBe('file');
      expect(dirent.mtime).toBe(1234567890);
      expect(dirent.permission).toBe('rw');
      expect(dirent.starred).toBe(true);
      expect(dirent.is_locked).toBe(true);
      expect(dirent.lock_time).toBe(1234567800);
      expect(dirent.lock_owner).toBe('user@example.com');
      expect(dirent.lock_owner_name).toBe('Test User');
      expect(dirent.locked_by_me).toBe(false);
      expect(dirent.modifier_name).toBe('Editor');
      expect(dirent.file_tags).toHaveLength(1);
      expect(dirent.file_tags[0].name).toBe('Important');
      expect(dirent.encoded_thumbnail_src).toBe('/thumbnail/abc123');
      expect(dirent.isSelected).toBe(false);
    });

    test('creates directory dirent', () => {
      const json = {
        id: 'dir123',
        name: 'Documents',
        type: 'dir',
        mtime: 1234567890,
        permission: 'rw'
      };

      const dirent = new Dirent(json);

      expect(dirent.id).toBe('dir123');
      expect(dirent.name).toBe('Documents');
      expect(dirent.type).toBe('dir');
      expect(dirent.has_been_shared_out).toBe(false);
      expect(dirent.is_locked).toBeUndefined();
      expect(dirent.file_tags).toBeUndefined();
    });

    test('uses defaults for missing fields', () => {
      const json = {
        name: 'minimal.txt',
        type: 'file',
        mtime: 1234567890,
        size: 0
      };

      const dirent = new Dirent(json);

      expect(dirent.id).toBe('0000000000000000');
      expect(dirent.permission).toBe('rw');
      expect(dirent.starred).toBe(false);
      expect(dirent.is_locked).toBe(false);
      expect(dirent.lock_owner).toBeNull();
      expect(dirent.modifier_name).toBe('');
      expect(dirent.file_tags).toEqual([]);
    });

    test('handles zero mtime', () => {
      const json = {
        name: 'new.txt',
        type: 'file',
        mtime: 0,
        size: 100
      };

      const dirent = new Dirent(json);

      expect(dirent.mtime).toBe(0);
      expect(dirent.mtime_relative).toBe('');
    });

    test('handles missing file_tags', () => {
      const json = {
        name: 'notags.txt',
        type: 'file',
        mtime: 1234567890,
        size: 100
      };

      const dirent = new Dirent(json);

      expect(dirent.file_tags).toEqual([]);
    });
  });

  describe('clone', () => {
    test('creates independent copy', () => {
      const json = {
        id: 'abc123',
        name: 'test.txt',
        type: 'file',
        mtime: 1234567890,
        size: 1024,
        starred: true
      };

      const original = new Dirent(json);
      const clone = original.clone();

      expect(clone.id).toBe(original.id);
      expect(clone.name).toBe(original.name);
      expect(clone).not.toBe(original);
    });
  });

  describe('isDir', () => {
    test('returns true for directories', () => {
      const dir = new Dirent({ name: 'folder', type: 'dir', mtime: 0 });
      expect(dir.isDir()).toBe(true);
    });

    test('returns false for files', () => {
      const file = new Dirent({ name: 'file.txt', type: 'file', mtime: 0, size: 0 });
      expect(file.isDir()).toBe(false);
    });
  });
});
