import React from 'react';
import PropTypes from 'prop-types';
import { siteRoot, enableVideoThumbnail, gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import toaster from '../toast';
import Dirent from '../../models/dirent';
import DetailListView from './detail-list-view';
import FileHistoryPanel from './file-history-panel';

import '../../css/dirent-detail.css';

const propTypes = {
  repoID: PropTypes.string.isRequired,
  dirent: PropTypes.object,
  path: PropTypes.string.isRequired,
  currentRepoInfo: PropTypes.object.isRequired,
  onItemDetailsClose: PropTypes.func.isRequired,
  onFileTagChanged: PropTypes.func.isRequired,
  direntDetailPanelTab: PropTypes.string,
  repoTags: PropTypes.array,
  fileTags: PropTypes.array,
};

class DirentDetail extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      direntType: '',
      direntDetail: '',
      folderDirent: null,
      activeTab: props.direntDetailPanelTab === 'history' ? 'history' : 'info',
    };
  }

  componentDidMount() {
    let { dirent, path, repoID } = this.props;
    this.loadDirentInfo(dirent, path, repoID);
  }

  UNSAFE_componentWillReceiveProps(nextProps) {
    let { dirent, path, repoID } = nextProps;
    if (this.props.dirent !== nextProps.dirent) {
      this.loadDirentInfo(dirent, path, repoID);
      this.setState({ activeTab: 'info' });
    }
    if (nextProps.direntDetailPanelTab === 'history' && this.props.direntDetailPanelTab !== 'history') {
      this.setState({ activeTab: 'history' });
    }
  }

  loadDirentInfo = (dirent, path, repoID) => {
    if (dirent) {
      let direntPath = Utils.joinPath(path, dirent.name);
      this.updateDetailView(dirent, direntPath);
    } else {
      let dirPath = Utils.getDirName(path);
      seafileAPI.listDir(repoID, dirPath).then(res => {
        let direntList = res.data.dirent_list;
        let folderDirent = null;
        for (let i = 0; i < direntList.length; i++) {
          let dirent = direntList[i];
          if (dirent.parent_dir + dirent.name === path) {
            folderDirent = new Dirent(dirent);
            break;
          }
        }
        this.setState({folderDirent: folderDirent});
        this.updateDetailView(folderDirent, path);
      }).catch(error => {
        let errMessage = Utils.getErrorMsg(error);
        toaster.danger(errMessage);
      });
    }
  };

  updateDetailView = (dirent, direntPath) => {
    let repoID = this.props.repoID;
    if (dirent.type === 'file') {
      seafileAPI.getFileInfo(repoID, direntPath).then(res => {
        this.setState({
          direntType: 'file',
          direntDetail: res.data,
        });
      }).catch(error => {
        let errMessage = Utils.getErrorMsg(error);
        toaster.danger(errMessage);
      });
    } else {
      seafileAPI.getDirInfo(repoID, direntPath).then(res => {
        this.setState({
          direntType: 'dir',
          direntDetail: res.data
        });
      }).catch(error => {
        let errMessage = Utils.getErrorMsg(error);
        toaster.danger(errMessage);
      });
    }
  };

  switchTab = (tab) => {
    this.setState({ activeTab: tab });
  };

  renderHeader = (smallIconUrl, direntName, isFile) => {
    const { activeTab } = this.state;
    return (
      <div className="detail-header-wrapper">
        <div className="detail-header">
          <div className="detail-control sf2-icon-x1" onClick={this.props.onItemDetailsClose}></div>
          <div className="detail-title dirent-title">
            <img src={smallIconUrl} width="24" height="24" alt="" />{' '}
            <span className="name ellipsis" title={direntName}>{direntName}</span>
          </div>
        </div>
        {isFile && (
          <div className="detail-tabs">
            <button
              className={`detail-tab${activeTab === 'info' ? ' active' : ''}`}
              onClick={() => this.switchTab('info')}
            >
              {gettext('Info')}
            </button>
            <button
              className={`detail-tab${activeTab === 'history' ? ' active' : ''}`}
              onClick={() => this.switchTab('history')}
            >
              {gettext('History')}
            </button>
          </div>
        )}
      </div>
    );
  };

  getFilePath = () => {
    const { dirent, path } = this.props;
    const { folderDirent } = this.state;
    const d = dirent || folderDirent;
    if (!d) return path;
    return Utils.joinPath(path, d.name);
  };

  renderDetailBody = (bigIconUrl, folderDirent) => {
    const { dirent, fileTags } = this.props;
    const { activeTab, direntType } = this.state;
    const isFile = direntType === 'file';

    if (isFile && activeTab === 'history') {
      return (
        <div className="detail-body">
          <FileHistoryPanel
            repoID={this.props.repoID}
            filePath={this.getFilePath()}
          />
        </div>
      );
    }

    return (
      <div className="detail-body dirent-info">
        <div className="img"><img src={bigIconUrl} className="thumbnail" alt="" /></div>
        {this.state.direntDetail &&
          <div className="dirent-table-container">
            <DetailListView
              repoInfo={this.props.currentRepoInfo}
              path={this.props.path}
              repoID={this.props.repoID}
              dirent={this.props.dirent || folderDirent}
              direntType={this.state.direntType}
              direntDetail={this.state.direntDetail}
              repoTags={this.props.repoTags}
              fileTagList={dirent ? dirent.file_tags : fileTags}
              onFileTagChanged={this.props.onFileTagChanged}
            />
          </div>
        }
      </div>
    );
  };

  render() {
    let { dirent, repoID, path } = this.props;
    let { folderDirent } = this.state;
    if (!dirent && !folderDirent) {
      return '';
    }
    let smallIconUrl = dirent ? Utils.getDirentIcon(dirent) : Utils.getDirentIcon(folderDirent);
    let bigIconUrl = dirent ? Utils.getDirentIcon(dirent, true) : Utils.getDirentIcon(folderDirent, true);
    const isImg = dirent ? Utils.imageCheck(dirent.name) : Utils.imageCheck(folderDirent.name);
    const isVideo = dirent ? Utils.videoCheck(dirent.name) : Utils.videoCheck(folderDirent.name);
    if (isImg || (enableVideoThumbnail && isVideo)) {
      bigIconUrl = `${siteRoot}thumbnail/${repoID}/1024` + Utils.encodePath(`${path === '/' ? '' : path}/${dirent.name}`);
    }
    let direntName = dirent ? dirent.name : folderDirent.name;
    let isFile = this.state.direntType === 'file';
    return (
      <div className="detail-container">
        {this.renderHeader(smallIconUrl, direntName, isFile)}
        {this.renderDetailBody(bigIconUrl, folderDirent)}
      </div>
    );
  }
}

DirentDetail.propTypes = propTypes;

export default DirentDetail;
