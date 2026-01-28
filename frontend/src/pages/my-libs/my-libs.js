import React, { Component, Fragment } from 'react';
import PropTypes from 'prop-types';
import cookie from 'react-cookies';
import { seafileAPI } from '../../utils/seafile-api';
import { gettext } from '../../utils/constants';
import { Utils } from '../../utils/utils';
import toaster from '../../components/toast';
import Repo from '../../models/repo';
import Loading from '../../components/loading';
import EmptyTip from '../../components/empty-tip';
import CommonToolbar from '../../components/toolbar/common-toolbar';
import RepoViewToolbar from '../../components/toolbar/repo-view-toobar';
import LibDetail from '../../components/dirent-detail/lib-details';
import MylibRepoListView from './mylib-repo-list-view';
import SortOptionsDialog from '../../components/dialog/sort-options';
import GuideForNewDialog from '../../components/dialog/guide-for-new-dialog';
import ModalPortal from '../../components/modal-portal';
import BatchDeleteRepoDialog from '../../components/dialog/batch-delete-repo-dialog';

const propTypes = {
  onShowSidePanel: PropTypes.func.isRequired,
  onSearchedClick: PropTypes.func.isRequired,
};

class MyLibraries extends Component {
  constructor(props) {
    super(props);
    this.state = {
      errorMsg: '',
      isLoading: true,
      repoList: [],
      selectedRepos: [],
      isAllSelected: false,
      isBatchDeleteDialogOpen: false,
      isShowDetails: false,
      isSortOptionsDialogOpen: false,
      isGuideForNewDialogOpen: window.app.pageOptions.guideEnabled,
      sortBy: cookie.load('seafile-repo-dir-sort-by') || 'name', // 'name' or 'time' or 'size'
      sortOrder: cookie.load('seafile-repo-dir-sort-order') || 'asc', // 'asc' or 'desc'
    };

  }

  getEmptyTip = () => {
    // Check permission dynamically (not from import, as it updates after API call)
    const userCanAddRepo = window.app.pageOptions.canAddRepo;

    if (userCanAddRepo) {
      return (
        <EmptyTip>
          <h2>{gettext('No libraries')}</h2>
          <p>{gettext('You have not created any libraries yet. A library is a container to organize your files and folders. A library can also be shared with others and synced to your connected devices. You can create a library by clicking the "New Library" button in the menu bar.')}</p>
        </EmptyTip>
      );
    } else {
      return (
        <EmptyTip>
          <h2>{gettext('No libraries')}</h2>
          <p>{gettext('You do not have any libraries. Libraries shared with you will appear here.')}</p>
        </EmptyTip>
      );
    }
  };

  componentDidMount() {
    seafileAPI.listRepos({type: 'mine'}).then((res) => {
      let repoList = res.data.repos.map((item) => {
        return new Repo(item);
      });
      this.setState({
        isLoading: false,
        repoList: Utils.sortRepos(repoList, this.state.sortBy, this.state.sortOrder)
      });
    }).catch((error) => {
      this.setState({
        isLoading: false,
        errorMsg: Utils.getErrorMsg(error, true) // true: show login tip if 403
      });
    });
  }

  toggleSortOptionsDialog = () => {
    this.setState({
      isSortOptionsDialogOpen: !this.state.isSortOptionsDialogOpen
    });
  };

  onCreateRepo = (repo) => {
    seafileAPI.createMineRepo(repo).then((res) => {
      const newRepo = new Repo({
        repo_id: res.data.repo_id,
        repo_name: res.data.repo_name,
        size: res.data.repo_size,
        mtime: res.data.mtime,
        owner_email: res.data.email,
        encrypted: res.data.encrypted,
        permission: res.data.permission,
        storage_name: res.data.storage_name
      });
      this.state.repoList.unshift(newRepo);
      this.setState({repoList: this.state.repoList});
    }).catch(error => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  sortRepoList = (sortBy, sortOrder) => {
    cookie.save('seafile-repo-dir-sort-by', sortBy);
    cookie.save('seafile-repo-dir-sort-order', sortOrder);
    this.setState({
      sortBy: sortBy,
      sortOrder: sortOrder,
      repoList: Utils.sortRepos(this.state.repoList, sortBy, sortOrder)
    });
  };

  onTransferRepo = (repoID) => {
    let repoList = this.state.repoList.filter(item => {
      return item.repo_id !== repoID;
    });
    this.setState({repoList: repoList});
  };

  onRenameRepo = (repo, newName) => {
    let repoList = this.state.repoList.map(item => {
      if (item.repo_id === repo.repo_id) {
        item.repo_name = newName;
      }
      return item;
    });
    this.setState({repoList: repoList});
  };

  onMonitorRepo = (repo, monitored) => {
    let repoList = this.state.repoList.map(item => {
      if (item.repo_id === repo.repo_id) {
        item.monitored = monitored;
      }
      return item;
    });
    this.setState({repoList: repoList});
  };

  onDeleteRepo = (repo) => {
    let repoList = this.state.repoList.filter(item => {
      return item.repo_id !== repo.repo_id;
    });
    this.setState({repoList: repoList});
  };

  // Selection methods for batch operations
  onSelectRepo = (repo, isSelected) => {
    let selectedRepos;
    if (isSelected) {
      selectedRepos = [...this.state.selectedRepos, repo];
    } else {
      selectedRepos = this.state.selectedRepos.filter(item => item.repo_id !== repo.repo_id);
    }
    const isAllSelected = selectedRepos.length === this.state.repoList.length;
    this.setState({ selectedRepos, isAllSelected });
  };

  onSelectAllRepos = (isSelected) => {
    if (isSelected) {
      this.setState({
        selectedRepos: [...this.state.repoList],
        isAllSelected: true
      });
    } else {
      this.setState({
        selectedRepos: [],
        isAllSelected: false
      });
    }
  };

  isRepoSelected = (repo) => {
    return this.state.selectedRepos.some(item => item.repo_id === repo.repo_id);
  };

  toggleBatchDeleteDialog = () => {
    this.setState({ isBatchDeleteDialogOpen: !this.state.isBatchDeleteDialogOpen });
  };

  onBatchDeleteRepos = (repos) => {
    const deletePromises = repos.map(repo =>
      seafileAPI.deleteRepo(repo.repo_id).catch(error => ({ error, repo }))
    );

    Promise.all(deletePromises).then(results => {
      const errors = results.filter(r => r && r.error);
      const successCount = repos.length - errors.length;

      // Remove successfully deleted repos from list
      const deletedRepoIds = repos
        .filter(repo => !errors.some(e => e.repo && e.repo.repo_id === repo.repo_id))
        .map(repo => repo.repo_id);

      const repoList = this.state.repoList.filter(item => !deletedRepoIds.includes(item.repo_id));

      this.setState({
        repoList,
        selectedRepos: [],
        isAllSelected: false,
        isBatchDeleteDialogOpen: false
      });

      if (errors.length === 0) {
        const msg = successCount === 1
          ? gettext('Successfully deleted 1 library.')
          : gettext('Successfully deleted {count} libraries.').replace('{count}', successCount);
        toaster.success(msg);
      } else if (successCount > 0) {
        const msg = gettext('Deleted {success} libraries, {failed} failed.')
          .replace('{success}', successCount)
          .replace('{failed}', errors.length);
        toaster.warning(msg);
      } else {
        toaster.danger(gettext('Failed to delete libraries.'));
      }
    });
  };

  onRepoClick = (repo) => {
    if (this.state.isShowDetails) {
      this.onRepoDetails(repo);
    }
  };

  onRepoDetails = (repo) => {
    this.setState({
      currentRepo: repo,
      isShowDetails: true,
    });
  };

  closeDetails = () => {
    this.setState({isShowDetails: !this.state.isShowDetails});
  };

  toggleGuideForNewDialog = () => {
    window.app.pageOptions.guideEnabled = false;
    this.setState({
      isGuideForNewDialogOpen: false
    });
  };

  render() {
    return (
      <Fragment>
        <div className="main-panel-north border-left-show">
          <RepoViewToolbar onShowSidePanel={this.props.onShowSidePanel} onCreateRepo={this.onCreateRepo} libraryType={'mine'}/>
          <CommonToolbar onSearchedClick={this.props.onSearchedClick} />
        </div>
        <div className="main-panel-center flex-row">
          <div className="cur-view-container">
            <div className="cur-view-path">
              <h3 className="sf-heading m-0">{gettext('My Libraries')}</h3>
              {(!Utils.isDesktop() && this.state.repoList.length > 0) && <span className="sf3-font sf3-font-sort action-icon" onClick={this.toggleSortOptionsDialog}></span>}
            </div>
            <div className="cur-view-content">
              {this.state.isLoading && <Loading />}
              {!this.state.isLoading && this.state.errorMsg && <p className="error text-center mt-8">{this.state.errorMsg}</p>}
              {!this.state.isLoading && !this.state.errorMsg && this.state.repoList.length === 0 && this.getEmptyTip()}
              {!this.state.isLoading && !this.state.errorMsg && this.state.repoList.length > 0 &&
                <Fragment>
                  {this.state.selectedRepos.length > 0 && (
                    <div className="selected-items-toolbar d-flex align-items-center p-2 mb-2" style={{ backgroundColor: '#f5f5f5', borderRadius: '4px' }}>
                      <span className="mr-3">
                        {gettext('{count} selected').replace('{count}', this.state.selectedRepos.length)}
                      </span>
                      <button
                        className="btn btn-secondary btn-sm mr-2"
                        onClick={() => this.onSelectAllRepos(false)}
                      >
                        {gettext('Cancel')}
                      </button>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={this.toggleBatchDeleteDialog}
                      >
                        <i className="sf2-icon-delete mr-1"></i>
                        {gettext('Delete')}
                      </button>
                    </div>
                  )}
                  <MylibRepoListView
                    sortBy={this.state.sortBy}
                    sortOrder={this.state.sortOrder}
                    repoList={this.state.repoList}
                    selectedRepos={this.state.selectedRepos}
                    isAllSelected={this.state.isAllSelected}
                    onSelectRepo={this.onSelectRepo}
                    onSelectAllRepos={this.onSelectAllRepos}
                    isRepoSelected={this.isRepoSelected}
                    onRenameRepo={this.onRenameRepo}
                    onDeleteRepo={this.onDeleteRepo}
                    onTransferRepo={this.onTransferRepo}
                    onMonitorRepo={this.onMonitorRepo}
                    onRepoClick={this.onRepoClick}
                    sortRepoList={this.sortRepoList}
                  />
                </Fragment>
              }
            </div>
          </div>
          {!this.state.isLoading && !this.state.errorMsg && this.state.isGuideForNewDialogOpen &&
            <GuideForNewDialog
              toggleDialog={this.toggleGuideForNewDialog}
            />
          }
          {this.state.isSortOptionsDialogOpen &&
            <SortOptionsDialog
              toggleDialog={this.toggleSortOptionsDialog}
              sortBy={this.state.sortBy}
              sortOrder={this.state.sortOrder}
              sortItems={this.sortRepoList}
            />
          }
          {this.state.isShowDetails && (
            <div className="cur-view-detail">
              <LibDetail
                currentRepo={this.state.currentRepo}
                closeDetails={this.closeDetails}
              />
            </div>
          )}
          {this.state.isBatchDeleteDialogOpen && (
            <ModalPortal>
              <BatchDeleteRepoDialog
                repos={this.state.selectedRepos}
                toggle={this.toggleBatchDeleteDialog}
                onDeleteRepos={this.onBatchDeleteRepos}
              />
            </ModalPortal>
          )}
        </div>
      </Fragment>
    );
  }
}

MyLibraries.propTypes = propTypes;

export default MyLibraries;
