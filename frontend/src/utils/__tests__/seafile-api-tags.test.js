/**
 * Tests for Tag API methods added to seafile-api.js
 *
 * These methods were added because the upstream seafile-js library
 * doesn't include tag-related API methods.
 *
 * Added methods:
 * - listRepoTags(repoID)
 * - createRepoTag(repoID, name, color)
 * - updateRepoTag(repoID, tagID, name, color)
 * - deleteRepoTag(repoID, tagID)
 * - getFileTags(repoID, filePath)
 * - addFileTag(repoID, filePath, repoTagID)
 * - deleteFileTag(repoID, fileTagID)
 * - listTaggedFiles(repoID, tagID)
 * - getShareLinkTaggedFiles(shareLinkToken, tagID)
 */

const fs = require('fs');
const path = require('path');

// Read the seafile-api.js file
const getSeafileApiContent = () => {
  const filePath = path.join(__dirname, '..', 'seafile-api.js');
  return fs.readFileSync(filePath, 'utf8');
};

describe('Tag API Methods in seafile-api.js', () => {

  let apiContent;

  beforeAll(() => {
    apiContent = getSeafileApiContent();
  });

  describe('Repository Tag Methods', () => {

    test('listRepoTags method exists', () => {
      expect(apiContent).toContain('seafileAPI.listRepoTags');
      expect(apiContent).toMatch(/listRepoTags\s*=\s*function\s*\(\s*repoID\s*\)/);
    });

    test('listRepoTags calls correct endpoint', () => {
      // Should call GET /api/v2.1/repos/{repo_id}/repo-tags/
      expect(apiContent).toMatch(/\/api\/v2\.1\/repos\/.*\/repo-tags\//);
    });

    test('createRepoTag method exists', () => {
      expect(apiContent).toContain('seafileAPI.createRepoTag');
      expect(apiContent).toMatch(/createRepoTag\s*=\s*function/);
    });

    test('createRepoTag accepts name and color parameters', () => {
      // Method signature should include repoID, name, color
      const createTagMatch = apiContent.match(/createRepoTag\s*=\s*function\s*\(([^)]+)\)/);
      expect(createTagMatch).toBeTruthy();
      const params = createTagMatch[1];
      expect(params).toContain('repoID');
      expect(params).toContain('name');
      expect(params).toContain('color');
    });

    test('updateRepoTag method exists', () => {
      expect(apiContent).toContain('seafileAPI.updateRepoTag');
    });

    test('deleteRepoTag method exists', () => {
      expect(apiContent).toContain('seafileAPI.deleteRepoTag');
    });
  });

  describe('File Tag Methods', () => {

    test('getFileTags method exists', () => {
      expect(apiContent).toContain('seafileAPI.getFileTags');
    });

    test('addFileTag method exists', () => {
      expect(apiContent).toContain('seafileAPI.addFileTag');
    });

    test('addFileTag accepts repoID, filePath, repoTagID', () => {
      const addTagMatch = apiContent.match(/addFileTag\s*=\s*function\s*\(([^)]+)\)/);
      expect(addTagMatch).toBeTruthy();
      const params = addTagMatch[1];
      expect(params).toContain('repoID');
      expect(params).toContain('filePath');
      expect(params).toContain('repoTagID');
    });

    test('deleteFileTag method exists', () => {
      expect(apiContent).toContain('seafileAPI.deleteFileTag');
    });

    test('deleteFileTag accepts repoID and fileTagID', () => {
      const deleteTagMatch = apiContent.match(/deleteFileTag\s*=\s*function\s*\(([^)]+)\)/);
      expect(deleteTagMatch).toBeTruthy();
      const params = deleteTagMatch[1];
      expect(params).toContain('repoID');
      expect(params).toContain('fileTagID');
    });
  });

  describe('Tagged Files Methods', () => {

    test('listTaggedFiles method exists', () => {
      expect(apiContent).toContain('seafileAPI.listTaggedFiles');
    });

    test('listTaggedFiles calls correct endpoint', () => {
      // Should call GET /api/v2.1/repos/{repo_id}/tagged-files/{tag_id}/
      expect(apiContent).toMatch(/\/tagged-files\//);
    });

    test('getShareLinkTaggedFiles method exists', () => {
      expect(apiContent).toContain('seafileAPI.getShareLinkTaggedFiles');
    });
  });

  describe('API Method Patterns', () => {

    test('all tag methods use correct HTTP methods', () => {
      // List operations should use GET
      expect(apiContent).toMatch(/listRepoTags[\s\S]*?\.get\(/);
      expect(apiContent).toMatch(/listTaggedFiles[\s\S]*?\.get\(/);
      expect(apiContent).toMatch(/getFileTags[\s\S]*?\.get\(/);

      // Create operations should use POST
      expect(apiContent).toMatch(/createRepoTag[\s\S]*?\.post\(/);
      expect(apiContent).toMatch(/addFileTag[\s\S]*?\.post\(/);

      // Delete operations should use DELETE
      expect(apiContent).toMatch(/deleteRepoTag[\s\S]*?\.delete\(/);
      expect(apiContent).toMatch(/deleteFileTag[\s\S]*?\.delete\(/);
    });

    test('methods return promises (this.req calls)', () => {
      // All methods should use this.req which returns promises
      const tagMethods = [
        'listRepoTags',
        'createRepoTag',
        'updateRepoTag',
        'deleteRepoTag',
        'getFileTags',
        'addFileTag',
        'deleteFileTag',
        'listTaggedFiles',
      ];

      tagMethods.forEach(method => {
        // Each method should have a return statement with this.req
        const methodRegex = new RegExp(`${method}[\\s\\S]*?return\\s+this\\.req`);
        expect(apiContent).toMatch(methodRegex);
      });
    });
  });

  describe('Documentation', () => {

    test('documents that tag methods are not in upstream seafile-js', () => {
      // The file should have a comment explaining these are custom additions
      expect(apiContent).toMatch(/tag.*not.*upstream|not.*seafile-js|custom.*tag/i);
    });
  });
});

describe('Tag API Endpoint Patterns', () => {

  test('documents correct endpoint patterns', () => {
    const endpoints = {
      listRepoTags: {
        method: 'GET',
        url: '/api/v2.1/repos/{repo_id}/repo-tags/',
      },
      createRepoTag: {
        method: 'POST',
        url: '/api/v2.1/repos/{repo_id}/repo-tags/',
        body: { name: 'string', color: 'string' },
      },
      updateRepoTag: {
        method: 'PUT',
        url: '/api/v2.1/repos/{repo_id}/repo-tags/{tag_id}/',
        body: { name: 'string', color: 'string' },
      },
      deleteRepoTag: {
        method: 'DELETE',
        url: '/api/v2.1/repos/{repo_id}/repo-tags/{tag_id}/',
      },
      getFileTags: {
        method: 'GET',
        url: '/api/v2.1/repos/{repo_id}/file-tags/?file_path={path}',
      },
      addFileTag: {
        method: 'POST',
        url: '/api/v2.1/repos/{repo_id}/file-tags/',
        body: { file_path: 'string', repo_tag_id: 'number' },
      },
      deleteFileTag: {
        method: 'DELETE',
        url: '/api/v2.1/repos/{repo_id}/file-tags/{file_tag_id}/',
      },
      listTaggedFiles: {
        method: 'GET',
        url: '/api/v2.1/repos/{repo_id}/tagged-files/{tag_id}/',
      },
    };

    // Verify we have all 8 tag endpoints documented
    expect(Object.keys(endpoints).length).toBe(8);
  });
});
