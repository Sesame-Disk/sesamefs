import React, { Fragment } from 'react';
import PropTypes from 'prop-types';
import moment from 'moment';
import { gettext, siteRoot } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import Loading from '../../components/loading';
import toaster from '../../components/toast';
import CommonToolbar from '../../components/toolbar/common-toolbar';

class RepoTrash extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      isLoading: true,
      errorMsg: '',
      repoName: '',
      items: [],
      scanStat: null,
      more: false,
      showFolder: false,
      commitID: '',
      baseDir: '',
      folderPath: '',
      folderItems: [],
    };
  }

  componentDidMount() {
    const { repoID } = this.props;

    // Fetch library name
    seafileAPI.getRepoInfo(repoID).then(res => {
      this.setState({ repoName: res.data.repo_name || res.data.name || 'Library' });
    }).catch(() => {
      this.setState({ repoName: 'Library' });
    });

    this.getItems();
  }

  getPath = () => {
    const urlParams = new URLSearchParams(window.location.search);
    return urlParams.get('path') || '/';
  };

  getItems = (scanStat) => {
    const { repoID } = this.props;
    const path = this.getPath();

    seafileAPI.getRepoFolderTrash(repoID, path, scanStat).then((res) => {
      const { data, more, scan_stat } = res.data;
      if (!data.length && more) {
        this.getItems(scan_stat);
      } else {
        this.setState({
          isLoading: false,
          items: this.state.items.concat(data),
          more: more,
          scanStat: scan_stat
        });
      }
    }).catch((error) => {
      this.setState({
        isLoading: false,
        errorMsg: Utils.getErrorMsg(error, true)
      });
    });
  };

  getMore = () => {
    this.setState({ isLoading: true });
    this.getItems(this.state.scanStat);
  };

  refreshTrash = () => {
    this.setState({
      isLoading: true,
      errorMsg: '',
      items: [],
      scanStat: null,
      more: false,
      showFolder: false
    });
    this.getItems();
  };

  goBack = (e) => {
    e.preventDefault();
    window.history.back();
  };

  restoreItem = (item) => {
    const { repoID } = this.props;
    const { commit_id, parent_dir, obj_name } = item;
    const path = parent_dir + obj_name;
    const request = item.is_dir ?
      seafileAPI.restoreFolder(repoID, commit_id, path) :
      seafileAPI.restoreFile(repoID, commit_id, path);
    request.then(() => {
      this.setState({
        items: this.state.items.filter(i => i !== item)
      });
      toaster.success(gettext('Successfully restored 1 item.'));
    }).catch((error) => {
      let errorMsg = Utils.getErrorMsg(error);
      toaster.danger(errorMsg);
    });
  };

  renderFolder = (commitID, baseDir, folderPath) => {
    const { repoID } = this.props;
    this.setState({
      showFolder: true,
      commitID: commitID,
      baseDir: baseDir,
      folderPath: folderPath,
      folderItems: [],
      isLoading: true
    });

    seafileAPI.listCommitDir(repoID, commitID, `${baseDir.substr(0, baseDir.length - 1)}${folderPath}`).then((res) => {
      this.setState({
        isLoading: false,
        folderItems: res.data.dirent_list
      });
    }).catch((error) => {
      this.setState({
        isLoading: false,
        errorMsg: Utils.getErrorMsg(error)
      });
    });
  };

  clickRoot = (e) => {
    e.preventDefault();
    this.refreshTrash();
  };

  render() {
    const { repoID } = this.props;
    const { isLoading, errorMsg, items, more, repoName, showFolder, folderItems } = this.state;

    return (
      <Fragment>
        <div className="main-panel-north border-left-show">
          <div className="cur-view-toolbar">
            <span className="sf2-icon-menu hidden-md-up d-md-none side-nav-toggle" title={gettext('Side Nav Menu')}></span>
            <div className="operation">
              <button className="btn btn-secondary operation-item" onClick={this.goBack}>
                <i className="sf2-icon-back mr-1"></i>{gettext('Back')}
              </button>
            </div>
          </div>
          <CommonToolbar />
        </div>
        <div className="main-panel-center flex-row">
          <div className="cur-view-container">
            <div className="cur-view-path">
              <h3 className="sf-heading m-0">{repoName} {gettext('Trash')}</h3>
            </div>
            <div className="cur-view-content">
              {showFolder && (
                <p className="m-2">
                  <a href="#" onClick={this.clickRoot}>{repoName}</a>
                  <span className="mx-1">/</span>
                  <span>{this.state.folderPath}</span>
                </p>
              )}
              {isLoading && <Loading />}
              {errorMsg && <p className="error mt-6 text-center">{errorMsg}</p>}
              {!isLoading && !errorMsg && (
                <table className="table-hover">
                  <thead>
                    <tr>
                      <th width="4%"></th>
                      <th width="36%">{gettext('Name')}</th>
                      <th width="24%">{gettext('Original path')}</th>
                      <th width="14%">{gettext('Delete Time')}</th>
                      <th width="12%">{gettext('Size')}</th>
                      <th width="10%"></th>
                    </tr>
                  </thead>
                  <tbody>
                    {showFolder ? (
                      folderItems.map((item, index) => (
                        <FolderItem
                          key={index}
                          item={item}
                          repoID={repoID}
                          commitID={this.state.commitID}
                          baseDir={this.state.baseDir}
                          folderPath={this.state.folderPath}
                          renderFolder={this.renderFolder}
                        />
                      ))
                    ) : (
                      items.map((item, index) => (
                        <TrashItem
                          key={index}
                          item={item}
                          repoID={repoID}
                          restoreItem={this.restoreItem}
                          renderFolder={this.renderFolder}
                        />
                      ))
                    )}
                  </tbody>
                </table>
              )}
              {(!isLoading && !errorMsg && items.length === 0 && !showFolder) &&
                <p className="text-center mt-4 text-secondary">{gettext('No deleted items.')}</p>
              }
              {(more && !isLoading && !showFolder) && (
                <button className="btn btn-block btn-outline-secondary mt-4" onClick={this.getMore}>{gettext('More')}</button>
              )}
            </div>
          </div>
        </div>
      </Fragment>
    );
  }
}

RepoTrash.propTypes = {
  repoID: PropTypes.string.isRequired,
};

class TrashItem extends React.Component {
  constructor(props) {
    super(props);
    this.state = { isIconShown: false };
  }

  handleMouseOver = () => this.setState({ isIconShown: true });
  handleMouseOut = () => this.setState({ isIconShown: false });

  restoreItem = (e) => {
    e.preventDefault();
    this.props.restoreItem(this.props.item);
  };

  renderFolder = (e) => {
    e.preventDefault();
    const item = this.props.item;
    this.props.renderFolder(item.commit_id, item.parent_dir, Utils.joinPath('/', item.obj_name));
  };

  render() {
    const { item } = this.props;
    const { isIconShown } = this.state;

    return item.is_dir ? (
      <tr onMouseOver={this.handleMouseOver} onMouseOut={this.handleMouseOut}>
        <td className="text-center"><img src={Utils.getFolderIconUrl()} alt="" width="24" /></td>
        <td><a href="#" onClick={this.renderFolder}>{item.obj_name}</a></td>
        <td className="text-secondary">{item.parent_dir}</td>
        <td title={moment(item.deleted_time).format('LLLL')}>{moment(item.deleted_time).format('YYYY-MM-DD')}</td>
        <td></td>
        <td>
          <a href="#" className={isIconShown ? '' : 'invisible'} onClick={this.restoreItem}>{gettext('Restore')}</a>
        </td>
      </tr>
    ) : (
      <tr onMouseOver={this.handleMouseOver} onMouseOut={this.handleMouseOut}>
        <td className="text-center"><img src={Utils.getFileIconUrl(item.obj_name)} alt="" width="24" /></td>
        <td>{item.obj_name}</td>
        <td className="text-secondary">{item.parent_dir}</td>
        <td title={moment(item.deleted_time).format('LLLL')}>{moment(item.deleted_time).format('YYYY-MM-DD')}</td>
        <td>{Utils.bytesToSize(item.size)}</td>
        <td>
          <a href="#" className={isIconShown ? '' : 'invisible'} onClick={this.restoreItem}>{gettext('Restore')}</a>
        </td>
      </tr>
    );
  }
}

TrashItem.propTypes = {
  item: PropTypes.object.isRequired,
  repoID: PropTypes.string.isRequired,
  restoreItem: PropTypes.func.isRequired,
  renderFolder: PropTypes.func.isRequired,
};

class FolderItem extends React.Component {
  renderFolder = (e) => {
    e.preventDefault();
    const { item, commitID, baseDir, folderPath } = this.props;
    this.props.renderFolder(commitID, baseDir, Utils.joinPath(folderPath, item.name));
  };

  render() {
    const { item } = this.props;

    return item.type === 'dir' ? (
      <tr>
        <td className="text-center"><img src={Utils.getFolderIconUrl()} alt="" width="24" /></td>
        <td><a href="#" onClick={this.renderFolder}>{item.name}</a></td>
        <td className="text-secondary">{item.parent_dir}</td>
        <td></td>
        <td></td>
        <td></td>
      </tr>
    ) : (
      <tr>
        <td className="text-center"><img src={Utils.getFileIconUrl(item.name)} alt="" width="24" /></td>
        <td>{item.name}</td>
        <td className="text-secondary">{item.parent_dir}</td>
        <td></td>
        <td>{Utils.bytesToSize(item.size)}</td>
        <td></td>
      </tr>
    );
  }
}

FolderItem.propTypes = {
  item: PropTypes.object.isRequired,
  repoID: PropTypes.string.isRequired,
  commitID: PropTypes.string.isRequired,
  baseDir: PropTypes.string.isRequired,
  folderPath: PropTypes.string.isRequired,
  renderFolder: PropTypes.func.isRequired,
};

export default RepoTrash;
