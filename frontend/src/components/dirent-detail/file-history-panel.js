import React from 'react';
import PropTypes from 'prop-types';
import moment from 'moment';
import { Dropdown, DropdownToggle, DropdownMenu, DropdownItem } from 'reactstrap';
import { gettext, siteRoot, serviceURL } from '../../utils/constants';
import { seafileAPI, getToken } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import Loading from '../loading';
import toaster from '../toast';

const propTypes = {
  repoID: PropTypes.string.isRequired,
  filePath: PropTypes.string.isRequired,
};

class FileHistoryPanel extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      isLoading: true,
      historyList: [],
      hasMore: false,
      currentPage: 1,
      isLoadingMore: false,
      errorMsg: '',
    };
    this.perPage = 25;
  }

  componentDidMount() {
    this.loadHistory(1);
  }

  componentDidUpdate(prevProps) {
    if (prevProps.filePath !== this.props.filePath || prevProps.repoID !== this.props.repoID) {
      this.setState({ isLoading: true, historyList: [], currentPage: 1 });
      this.loadHistory(1);
    }
  }

  loadHistory = (page) => {
    const { repoID, filePath } = this.props;

    if (seafileAPI.listFileHistoryRecords) {
      seafileAPI.listFileHistoryRecords(repoID, filePath, page, this.perPage).then(res => {
        this.handleResponse(res.data, page);
      }).catch(err => {
        this.handleError(err);
      });
    } else {
      this.fetchDirect(repoID, filePath, page);
    }
  };

  fetchDirect = (repoID, filePath, page) => {
    const token = getToken();
    const server = serviceURL || window.location.origin;

    fetch(`${server}/api2/repo/file_revisions/${repoID}/?p=${encodeURIComponent(filePath)}&page=${page}&per_page=${this.perPage}`, {
      headers: { 'Authorization': `Token ${token}` }
    })
    .then(response => {
      if (!response.ok) throw new Error('Failed to fetch history');
      return response.json();
    })
    .then(data => {
      this.handleResponse(data, page);
    })
    .catch(err => {
      this.handleError(err);
    });
  };

  handleResponse = (data, page) => {
    const historyList = data.data || [];
    const totalCount = data.total_count || historyList.length;

    this.setState(prevState => ({
      isLoading: false,
      isLoadingMore: false,
      historyList: page === 1 ? historyList : [...prevState.historyList, ...historyList],
      hasMore: totalCount > (this.perPage * page),
      currentPage: page,
    }));
  };

  handleError = (err) => {
    console.error('Failed to load history:', err);
    this.setState({
      isLoading: false,
      isLoadingMore: false,
      errorMsg: gettext('Failed to load file history'),
    });
  };

  onScroll = (e) => {
    const { clientHeight, scrollHeight, scrollTop } = e.target;
    const isBottom = (clientHeight + scrollTop + 1 >= scrollHeight);
    if (isBottom && this.state.hasMore && !this.state.isLoadingMore) {
      const nextPage = this.state.currentPage + 1;
      this.setState({ isLoadingMore: true, currentPage: nextPage });
      this.loadHistory(nextPage);
    }
  };

  onRestore = (item) => {
    const { repoID, filePath } = this.props;

    if (seafileAPI.revertFile) {
      seafileAPI.revertFile(repoID, filePath, item.commit_id).then(() => {
        toaster.success(gettext('Successfully restored.'));
        this.setState({ isLoading: true, historyList: [] });
        this.loadHistory(1);
      }).catch(() => {
        toaster.danger(gettext('Failed to restore file.'));
      });
    } else {
      toaster.warning(gettext('Restore not available'));
    }
  };

  onDownload = (item) => {
    const { repoID, filePath } = this.props;
    const token = getToken();

    if (item.rev_file_id) {
      // Use the history download endpoint with the FS object ID
      const params = `obj_id=${item.rev_file_id}&p=${encodeURIComponent(filePath)}` + (token ? `&token=${token}` : '');
      const downloadUrl = `${siteRoot}repo/${repoID}/history/download?${params}`;
      window.open(downloadUrl);
      return;
    }

    // Fallback: fetch download URL via API (for items without rev_file_id)
    const server = serviceURL || window.location.origin;
    const apiUrl = `${server}/api2/repos/${repoID}/file/?p=${encodeURIComponent(filePath)}`;

    fetch(apiUrl, {
      headers: { 'Authorization': `Token ${token}` }
    })
    .then(response => {
      if (!response.ok) throw new Error('Failed to get download link');
      return response.text();
    })
    .then(downloadUrl => {
      const url = downloadUrl.replace(/"/g, '').trim();
      window.location.href = url;
    })
    .catch(() => {
      toaster.danger(gettext('Failed to download file.'));
    });
  };

  render() {
    const { repoID, filePath } = this.props;
    const { isLoading, historyList, errorMsg, isLoadingMore, hasMore } = this.state;

    if (isLoading) {
      return <div className="history-panel"><Loading /></div>;
    }

    if (errorMsg) {
      return <div className="history-panel"><p className="text-center text-danger mt-4">{errorMsg}</p></div>;
    }

    if (historyList.length === 0) {
      return <div className="history-panel"><p className="text-center text-secondary mt-4">{gettext('No history available')}</p></div>;
    }

    const fullHistoryUrl = `${siteRoot}repo/file_revisions/${repoID}/?p=${encodeURIComponent(filePath)}`;

    return (
      <div className="history-panel" onScroll={this.onScroll}>
        {historyList.map((item, index) => (
          <HistoryRecord
            key={item.commit_id + '-' + index}
            item={item}
            index={index}
            onRestore={this.onRestore}
            onDownload={this.onDownload}
          />
        ))}
        {isLoadingMore && <Loading />}
        {!hasMore && historyList.length > 0 && (
          <p className="text-center text-secondary mt-2 mb-2" style={{fontSize: '13px'}}>{gettext('No more history')}</p>
        )}
        <div className="text-center mt-2 mb-3">
          <a href={fullHistoryUrl} className="text-primary" style={{fontSize: '13px'}}>
            {gettext('View all history')}
          </a>
        </div>
      </div>
    );
  }
}

FileHistoryPanel.propTypes = propTypes;

class HistoryRecord extends React.Component {
  constructor(props) {
    super(props);
    this.state = { isMenuOpen: false };
  }

  toggleMenu = () => {
    this.setState(prevState => ({ isMenuOpen: !prevState.isMenuOpen }));
  };

  render() {
    const { item, index, onRestore, onDownload } = this.props;
    const { isMenuOpen } = this.state;

    const timeStr = moment.unix(item.ctime).fromNow();
    const creator = item.creator_name || item.creator_email || 'Unknown';
    const size = item.size ? Utils.bytesToSize(item.size) : '';

    return (
      <div className="history-record">
        <div className="history-record-top">
          <span className="history-record-time" title={moment.unix(item.ctime).format('YYYY-MM-DD HH:mm')}>{timeStr}</span>
          <Dropdown isOpen={isMenuOpen} toggle={this.toggleMenu} size="sm">
            <DropdownToggle tag="span" className="history-record-menu-toggle" data-toggle="dropdown">
              <i className="fas fa-ellipsis-h"></i>
            </DropdownToggle>
            <DropdownMenu right>
              {index !== 0 && (
                <DropdownItem onClick={() => onRestore(item)}>
                  <i className="fas fa-undo mr-2"></i>{gettext('Restore')}
                </DropdownItem>
              )}
              <DropdownItem onClick={() => onDownload(item)}>
                <i className="fas fa-download mr-2"></i>{gettext('Download')}
              </DropdownItem>
            </DropdownMenu>
          </Dropdown>
        </div>
        <div className="history-record-bottom">
          <span className="history-record-creator text-truncate" title={creator}>{creator}</span>
          {size && <span className="history-record-size">{size}</span>}
        </div>
      </div>
    );
  }
}

HistoryRecord.propTypes = {
  item: PropTypes.object.isRequired,
  index: PropTypes.number.isRequired,
  onRestore: PropTypes.func.isRequired,
  onDownload: PropTypes.func.isRequired,
};

export default FileHistoryPanel;
