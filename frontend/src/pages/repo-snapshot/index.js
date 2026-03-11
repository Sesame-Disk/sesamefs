import React, { Fragment } from 'react';
import PropTypes from 'prop-types';
import moment from 'moment';
import { gettext, siteRoot } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import Loading from '../../components/loading';
import toaster from '../../components/toast';
import CommonToolbar from '../../components/toolbar/common-toolbar';

class RepoSnapshot extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      isLoading: true,
      errorMsg: '',
      repoName: '',
      userPerm: 'rw',
      commitID: '',
      commitDesc: '',
      commitTime: null,
      commitAuthor: '',
      commitAuthorEmail: '',
      folderPath: '/',
      folderItems: [],
      // Track restored items to show visual feedback
      restoredItems: new Set(),
      // Conflict dialog state
      showConflictDialog: false,
      conflictItem: null,
      conflictPath: '',
    };
  }

  componentDidMount() {
    const { repoID } = this.props;

    // Parse commit_id from URL query params
    const urlParams = new URLSearchParams(window.location.search);
    const commitID = urlParams.get('commit_id') || '';

    if (!commitID) {
      this.setState({
        isLoading: false,
        errorMsg: gettext('Missing commit_id parameter')
      });
      return;
    }

    this.setState({ commitID });

    // Fetch library info
    seafileAPI.getRepoInfo(repoID).then(res => {
      this.setState({
        repoName: res.data.repo_name || res.data.name || 'Library',
        userPerm: res.data.permission || 'rw'
      });
    }).catch(() => {
      this.setState({ repoName: 'Library' });
    });

    // Fetch commit details from history
    this.fetchCommitDetails(repoID, commitID);

    // Fetch directory listing at this commit
    this.renderFolder(repoID, commitID, '/');
  }

  fetchCommitDetails = (repoID, commitID) => {
    // Get history and find the specific commit
    // We fetch more items to increase chances of finding it
    seafileAPI.getRepoHistory(repoID, 1, 100).then(res => {
      const commits = res.data.data || [];
      const commit = commits.find(c => c.commit_id === commitID);
      if (commit) {
        this.setState({
          commitDesc: commit.description || '',
          commitTime: commit.time,
          commitAuthor: commit.name || '',
          commitAuthorEmail: commit.email || ''
        });
      }
    }).catch(error => {
      console.error('Failed to fetch commit details:', error);
    });
  };

  renderFolder = (repoID, commitID, folderPath) => {
    this.setState({
      folderPath: folderPath,
      folderItems: [],
      isLoading: true,
      errorMsg: '',
      // Clear restored items when navigating to a different folder
      restoredItems: new Set()
    });

    seafileAPI.listCommitDir(repoID, commitID, folderPath).then((res) => {
      this.setState({
        isLoading: false,
        folderItems: res.data.dirent_list || []
      });
    }).catch((error) => {
      this.setState({
        isLoading: false,
        errorMsg: Utils.getErrorMsg(error, true)
      });
    });
  };

  clickFolderPath = (folderPath, e) => {
    e.preventDefault();
    const { commitID } = this.state;
    this.renderFolder(this.props.repoID, commitID, folderPath);
  };

  goBack = (e) => {
    e.preventDefault();
    window.history.back();
  };

  restoreRepo = () => {
    const { repoID } = this.props;
    const { commitID } = this.state;

    seafileAPI.revertRepo(repoID, commitID).then(() => {
      toaster.success(gettext('Successfully restored the library.'));
    }).catch((error) => {
      let errorMsg = Utils.getErrorMsg(error);
      toaster.danger(errorMsg);
    });
  };

  // Mark an item as restored (for visual feedback)
  markAsRestored = (itemName) => {
    this.setState(prevState => {
      const newSet = new Set(prevState.restoredItems);
      newSet.add(itemName);
      return { restoredItems: newSet };
    });
  };

  // Called by SnapshotItem when there's a conflict
  handleConflict = (item, path) => {
    this.setState({
      showConflictDialog: true,
      conflictItem: item,
      conflictPath: path
    });
  };

  closeConflictDialog = () => {
    this.setState({
      showConflictDialog: false,
      conflictItem: null,
      conflictPath: ''
    });
  };

  handleConflictReplace = () => {
    this.executeRestore('replace');
  };

  handleConflictKeepBoth = () => {
    this.executeRestore('keep_both');
  };

  handleConflictSkip = () => {
    toaster.notify(gettext('Skipped restoring the item.'));
    this.closeConflictDialog();
  };

  executeRestore = (conflictPolicy) => {
    const { repoID } = this.props;
    const { commitID, conflictItem, conflictPath } = this.state;

    const request = conflictItem.type === 'dir' ?
      seafileAPI.revertFolder(repoID, conflictPath, commitID, conflictPolicy) :
      seafileAPI.revertFile(repoID, conflictPath, commitID, conflictPolicy);

    request.then(() => {
      toaster.success(gettext('Successfully restored 1 item.'));
      this.markAsRestored(conflictItem.name);
      this.closeConflictDialog();
    }).catch((error) => {
      let errorMsg = Utils.getErrorMsg(error);
      toaster.danger(errorMsg);
      this.closeConflictDialog();
    });
  };

  renderPath = () => {
    const { folderPath, repoName } = this.state;
    const pathList = folderPath.split('/');

    if (folderPath === '/') {
      return <span className="text-truncate" title={repoName}>{repoName}</span>;
    }

    return (
      <Fragment>
        <a href="#" onClick={(e) => this.clickFolderPath('/', e)} className="text-truncate" title={repoName}>{repoName}</a>
        <span className="mx-1">/</span>
        {pathList.map((item, index) => {
          if (index > 0 && index !== pathList.length - 1) {
            return (
              <Fragment key={index}>
                <a href="#" onClick={(e) => this.clickFolderPath(pathList.slice(0, index + 1).join('/'), e)} className="text-truncate" title={pathList[index]}>{pathList[index]}</a>
                <span className="mx-1">/</span>
              </Fragment>
            );
          }
          return null;
        })}
        <span className="text-truncate" title={pathList[pathList.length - 1]}>{pathList[pathList.length - 1]}</span>
      </Fragment>
    );
  };

  render() {
    const { repoID } = this.props;
    const { isLoading, errorMsg, repoName, userPerm, folderPath, folderItems,
      commitID, commitDesc, commitTime, commitAuthor,
      showConflictDialog, conflictItem, restoredItems } = this.state;

    return (
      <Fragment>
        <div className="main-panel-north border-left-show">
          <div className="cur-view-toolbar">
            <span className="sf2-icon-menu hidden-md-up d-md-none side-nav-toggle" title={gettext('Side Nav Menu')}></span>
            <div className="operation">
              <button className="btn btn-secondary operation-item" onClick={this.goBack}>
                <i className="sf2-icon-back mr-1"></i>{gettext('Back')}
              </button>
              {userPerm === 'rw' && folderPath === '/' && (
                <button className="btn btn-secondary operation-item ml-2" onClick={this.restoreRepo}>
                  {gettext('Restore Library')}
                </button>
              )}
            </div>
          </div>
          <CommonToolbar onSearchedClick={Utils.handleSearchedItemClick} />
        </div>
        <div className="main-panel-center flex-row">
          <div className="cur-view-container">
            <div className="cur-view-path">
              <h3 className="sf-heading m-0">{repoName} {gettext('Snapshot')}</h3>
              {commitTime && (
                <span className="text-secondary ml-2">({moment(commitTime).format('YYYY-MM-DD HH:mm')})</span>
              )}
            </div>
            <div className="cur-view-content">
              {folderPath === '/' && commitDesc && (
                <div className="mb-3">
                  <p className="text-secondary m-0">{commitDesc}</p>
                  {commitAuthor && (
                    <p className="text-secondary m-0 small">{gettext('By')}: {commitAuthor}</p>
                  )}
                </div>
              )}
              <p className="mb-2">
                <span className="mr-1">{gettext('Current path:')}</span>
                {this.renderPath()}
              </p>
              {isLoading ? (
                <Loading />
              ) : errorMsg ? (
                <p className="error mt-6 text-center">{errorMsg}</p>
              ) : (
                <Fragment>
                  <table className="table-hover">
                    <thead>
                      <tr>
                        <th width="5%"></th>
                        <th width="55%">{gettext('Name')}</th>
                        <th width="20%">{gettext('Size')}</th>
                        <th width="20%"></th>
                      </tr>
                    </thead>
                    <tbody>
                      {folderItems.map((item, index) => (
                        <SnapshotItem
                          key={index}
                          item={item}
                          repoID={repoID}
                          commitID={commitID}
                          folderPath={folderPath}
                          userPerm={userPerm}
                          isRestored={restoredItems.has(item.name)}
                          renderFolder={(path) => this.renderFolder(repoID, commitID, path)}
                          onConflict={this.handleConflict}
                          onRestored={this.markAsRestored}
                        />
                      ))}
                    </tbody>
                  </table>
                  {folderItems.length === 0 && (
                    <p className="text-center mt-4 text-secondary">{gettext('Empty folder.')}</p>
                  )}
                </Fragment>
              )}
            </div>
          </div>
        </div>

        {/* Conflict Dialog */}
        {showConflictDialog && (
          <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
            <div className="modal-dialog modal-dialog-centered">
              <div className="modal-content">
                <div className="modal-header">
                  <h5 className="modal-title">{gettext('File Already Exists')}</h5>
                  <button type="button" className="close" onClick={this.closeConflictDialog}>
                    <span aria-hidden="true">&times;</span>
                  </button>
                </div>
                <div className="modal-body">
                  <p>
                    {gettext('A file with the name')} <strong>{conflictItem?.name}</strong> {gettext('already exists in the current library with different content.')}
                  </p>
                  <p>{gettext('What would you like to do?')}</p>
                </div>
                <div className="modal-footer">
                  <button type="button" className="btn btn-secondary" onClick={this.handleConflictSkip}>
                    {gettext('Skip')}
                  </button>
                  <button type="button" className="btn btn-outline-primary" onClick={this.handleConflictKeepBoth}>
                    {gettext('Keep Both')}
                  </button>
                  <button type="button" className="btn btn-primary" onClick={this.handleConflictReplace}>
                    {gettext('Replace')}
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </Fragment>
    );
  }
}

RepoSnapshot.propTypes = {
  repoID: PropTypes.string.isRequired,
};

class SnapshotItem extends React.Component {
  constructor(props) {
    super(props);
    this.state = { isIconShown: false };
  }

  handleMouseOver = () => this.setState({ isIconShown: true });
  handleMouseOut = () => this.setState({ isIconShown: false });

  restoreItem = (e) => {
    e.preventDefault();
    const { item, repoID, commitID, folderPath, onConflict, onRestored } = this.props;
    const path = Utils.joinPath(folderPath, item.name);

    const request = item.type === 'dir' ?
      seafileAPI.revertFolder(repoID, path, commitID) :
      seafileAPI.revertFile(repoID, path, commitID);

    request.then((res) => {
      if (res.data.skipped) {
        toaster.notify(res.data.message || gettext('File skipped.'));
      } else if (res.data.message && res.data.message.includes('same content')) {
        // File already matches - show as restored
        toaster.success(gettext('File is already up to date.'));
        onRestored(item.name);
      } else if (res.data.message) {
        toaster.success(res.data.message);
        onRestored(item.name);
      } else {
        toaster.success(gettext('Successfully restored 1 item.'));
        onRestored(item.name);
      }
    }).catch((error) => {
      // Check for conflict response (HTTP 409)
      if (error.response && error.response.status === 409) {
        onConflict(item, path);
      } else {
        let errorMsg = Utils.getErrorMsg(error);
        toaster.danger(errorMsg);
      }
    });
  };

  renderFolder = (e) => {
    e.preventDefault();
    const { item, folderPath, renderFolder } = this.props;
    renderFolder(Utils.joinPath(folderPath, item.name));
  };

  render() {
    const { item, repoID, commitID, folderPath, userPerm, isRestored } = this.props;
    const { isIconShown } = this.state;

    const restoredBadge = isRestored ? (
      <span className="badge badge-success ml-2" title={gettext('Restored')}>✓</span>
    ) : null;

    return item.type === 'dir' ? (
      <tr onMouseOver={this.handleMouseOver} onMouseOut={this.handleMouseOut}>
        <td className="text-center"><img src={Utils.getFolderIconUrl()} alt="" width="24" /></td>
        <td>
          <a href="#" onClick={this.renderFolder}>{item.name}</a>
          {restoredBadge}
        </td>
        <td></td>
        <td>
          {userPerm === 'rw' && !isRestored && (
            <a href="#" className={isIconShown ? '' : 'invisible'} onClick={this.restoreItem} title={gettext('Restore')}>
              {gettext('Restore')}
            </a>
          )}
          {isRestored && (
            <span className="text-success">{gettext('Restored')}</span>
          )}
        </td>
      </tr>
    ) : (
      <tr onMouseOver={this.handleMouseOver} onMouseOut={this.handleMouseOut}>
        <td className="text-center"><img src={Utils.getFileIconUrl(item.name)} alt="" width="24" /></td>
        <td>
          <a href={`${siteRoot}repo/${repoID}/snapshot/files/?obj_id=${item.obj_id}&commit_id=${commitID}&p=${encodeURIComponent(Utils.joinPath(folderPath, item.name))}`} target="_blank" rel="noreferrer">
            {item.name}
          </a>
          {restoredBadge}
        </td>
        <td>{Utils.bytesToSize(item.size)}</td>
        <td>
          {userPerm === 'rw' && !isRestored && (
            <a href="#" className={isIconShown ? '' : 'invisible'} onClick={this.restoreItem} title={gettext('Restore')}>
              {gettext('Restore')}
            </a>
          )}
          {isRestored && (
            <span className="text-success">{gettext('Restored')}</span>
          )}
        </td>
      </tr>
    );
  }
}

SnapshotItem.propTypes = {
  item: PropTypes.object.isRequired,
  repoID: PropTypes.string.isRequired,
  commitID: PropTypes.string.isRequired,
  folderPath: PropTypes.string.isRequired,
  userPerm: PropTypes.string.isRequired,
  isRestored: PropTypes.bool.isRequired,
  renderFolder: PropTypes.func.isRequired,
  onConflict: PropTypes.func.isRequired,
  onRestored: PropTypes.func.isRequired,
};

export default RepoSnapshot;
