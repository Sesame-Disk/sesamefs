import React, { useState } from 'react';
import { gettext } from '../../utils/constants';
import { FormGroup } from 'reactstrap';
import LinkDetails from './link-details';
import LinkCreation from './link-creation';
import LinkList from './link-list';
import RenderShareButtons from './share-social-media';
import ChinaShareInfo from './china-share-info';
import { changeLinkToChina } from '../../services/links';
import { shareLinkExpireDaysMin, shareLinkExpireDaysMax, shareLinkExpireDaysDefault } from '../../utils/constants';
import '../../services/css.css';

function GenerateShareLinkWithPassword({
  shareLinks,
  repoID,
  itemPath,
  itemType,
  userPerm,
  closeShareDialog,
  permissionOptions,
  currentPermission,
  onLinkCreated,
  onLinkUpdated,
  onLinkDeleted,
  onLinksDeleted,
}) {
  const [show, setShow] = useState(false);
  const [mode, setMode] = useState('');
  const [sharedLinkInfo, setSharedLinkInfo] = useState(null);

  const isExpireDaysNoLimit = (shareLinkExpireDaysMin === 0 && shareLinkExpireDaysMax === 0 && shareLinkExpireDaysDefault === 0);
  const defaultExpireDays = isExpireDaysNoLimit ? '' : shareLinkExpireDaysDefault;

  // Filter to password-protected links and transform to China domain
  const chinaLinks = (shareLinks || [])
    .filter(link => link.has_password)
    .map(link => changeLinkToChina({ ...link }));

  const chinaLinksCount = chinaLinks.length;

  const getButtonText = () => {
    if (show) return '▼ Hide';
    if (chinaLinksCount === 0) return '▶ Create Password-Protected Link';
    if (chinaLinksCount === 1) return `▶ View Link (${chinaLinksCount})`;
    return `▶ View Links (${chinaLinksCount})`;
  };

  const handleToggle = (e) => {
    e.preventDefault();
    if (chinaLinksCount === 0 && !show) {
      setShow(true);
      setMode('singleLinkCreation');
    } else {
      setShow(!show);
    }
  };

  // When a link is created in the China panel, delegate to parent
  const handleCreation = (newData) => {
    if (Array.isArray(newData)) {
      // batch
      onLinkCreated(newData);
      setMode('');
    } else {
      // single
      onLinkCreated(newData);
      const transformed = changeLinkToChina({ ...newData });
      setSharedLinkInfo(transformed);
      setMode('displayLinkDetails');
    }
  };

  // When a link is updated in the China panel, delegate to parent
  const handleUpdateLink = (link) => {
    onLinkUpdated(link);
    if (!link.has_password) {
      // Password removed — link no longer belongs in China panel
      setSharedLinkInfo(null);
      setMode('');
    } else {
      setSharedLinkInfo(changeLinkToChina({ ...link }));
    }
  };

  // When a link is deleted in the China panel, delegate to parent
  const handleDeleteLink = (token) => {
    onLinkDeleted(token);
    setSharedLinkInfo(null);
    setMode('');
  };

  const handleDeleteShareLinks = () => {
    const tokens = chinaLinks.filter(item => item.isSelected).map(link => link.token);
    onLinksDeleted(tokens);
  };

  const showLinkDetails = (link) => {
    setSharedLinkInfo(link);
    setMode(link ? 'displayLinkDetails' : '');
  };

  const renderChinaPanel = () => {
    switch (mode) {
      case 'displayLinkDetails':
        return (
          <LinkDetails
            sharedLinkInfo={sharedLinkInfo}
            permissionOptions={permissionOptions || []}
            defaultExpireDays={defaultExpireDays}
            showLinkDetails={showLinkDetails}
            updateLink={handleUpdateLink}
            deleteLink={handleDeleteLink}
            closeShareDialog={closeShareDialog}
          />
        );
      case 'singleLinkCreation':
        return (
          <LinkCreation
            type="single"
            repoID={repoID}
            itemPath={itemPath}
            userPerm={userPerm}
            permissionOptions={permissionOptions || []}
            currentPermission={currentPermission || ''}
            setMode={setMode}
            updateAfterCreation={handleCreation}
            forcePassword={true}
          />
        );
      default:
        return (
          <LinkList
            shareLinks={chinaLinks}
            permissionOptions={permissionOptions || []}
            setMode={setMode}
            showLinkDetails={showLinkDetails}
            toggleSelectAllLinks={() => { }}
            toggleSelectLink={() => { }}
            deleteShareLinks={handleDeleteShareLinks}
            deleteLink={handleDeleteLink}
            handleScroll={() => { }}
            isLoadingMore={false}
            hideBatchButton={true}
          />
        );
    }
  };

  return (
    <div className="china-share-wrapper">
      {chinaLinksCount === 0 && <ChinaShareInfo />}

      <div className="china-share-header">
        <div className="china-share-title">
          <span className="china-flag-icon">🇨🇳</span>
          <span className="china-title-text">
            {gettext('Share in China')}
            <small style={{ display: 'block', fontSize: '11px', opacity: 0.7, fontWeight: 'normal', marginTop: '2px' }}>
              🔍 {gettext('Filtered view: password-protected links')}
            </small>
          </span>
        </div>
        <button
          className="china-toggle-btn"
          onClick={handleToggle}
          aria-expanded={show}
        >
          {getButtonText()}
        </button>
      </div>

      {show && (
        <div className="china-share-panel">
          {chinaLinksCount === 0 && mode !== 'singleLinkCreation' && (
            <div style={{ padding: '12px', background: '#f0f8ff', borderRadius: '4px', marginBottom: '12px', fontSize: '13px', color: '#666' }}>
              ℹ️ {gettext('Create your first password-protected link to share with users in China')}
            </div>
          )}
          {renderChinaPanel()}
        </div>
      )}

      <FormGroup className="mb-0 mt-3">
        <dt className="text-secondary font-weight-normal">{gettext('Share in social media:')}</dt>
        <dd><RenderShareButtons shareLinks={shareLinks} itemPath={itemPath} itemType={itemType} /></dd>
      </FormGroup>
    </div>
  );
}

export default GenerateShareLinkWithPassword;
