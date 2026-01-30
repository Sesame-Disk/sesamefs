import React from 'react';
import PropTypes from 'prop-types';

import moment from 'moment';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import Loading from '../loading';

import '../../css/commit-details.css';

const propTypes = {
  repoID: PropTypes.string.isRequired,
  commitID: PropTypes.string.isRequired,
  commitTime: PropTypes.string.isRequired,
  toggleDialog: PropTypes.func.isRequired
};

class CommitDetails extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      isLoading: true,
      errorMsg: '',
    };
  }

  componentDidMount() {
    const {repoID, commitID} = this.props;
    seafileAPI.getCommitDetails(repoID, commitID).then((res) => {
      this.setState({
        isLoading: false,
        errorMsg: '',
        commitDetails: res.data
      });
    }).catch((error) => {
      let errorMsg = Utils.getErrorMsg(error);
      this.setState({
        isLoading: false,
        errorMsg: errorMsg
      });
    });
  }

  render() {
    const { toggleDialog, commitTime} = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Modification Details')}</h5>
              <button type="button" className="close" onClick={toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p className="small">{moment(commitTime).format('YYYY-MM-DD HH:mm:ss')}</p>
          <Content data={this.state} />
        </div>
      </div>
          </div>
        </div>
    );
  }
}

class Content extends React.Component {

  renderDetails = (data) => {
    const detailsData = [
      {type: 'new', title: gettext('New files')},
      {type: 'removed', title: gettext('Deleted files')},
      {type: 'renamed', title: gettext('Renamed or Moved files')},
      {type: 'modified', title: gettext('Modified files')},
      {type: 'newdir', title: gettext('New directories')},
      {type: 'deldir', title: gettext('Deleted directories')}
    ];

    let showDesc = true;
    for (let i = 0, len = detailsData.length; i < len; i++) {
      if (data[detailsData[i].type].length) {
        showDesc = false;
        break;
      }
    }
    if (showDesc) {
      return <p>{data.cmt_desc}</p>;
    }

    return (
      <React.Fragment>
        {detailsData.map((item, index) => {
          if (!data[item.type].length) {
            return null;
          }
          return (
            <React.Fragment key={index}>
              <h6>{item.title}</h6>
              <ul>
                {
                  data[item.type].map((item, index) => {
                    return <li key={index} dangerouslySetInnerHTML={{__html: item}} className="commit-detail-item text-truncate"></li>;
                  })
                }
              </ul>
            </React.Fragment>
          );
        })}
      </React.Fragment>
    );
  };

  render() {
    const {isLoading, errorMsg, commitDetails} = this.props.data;

    if (isLoading) {
      return <Loading />;
    }

    if (errorMsg) {
      return <p className="error mt-4 text-center">{errorMsg}</p>;
    }

    return this.renderDetails(commitDetails);
  }
}

Content.propTypes = {
  data: PropTypes.object.isRequired,
};

CommitDetails.propTypes = propTypes;

export default CommitDetails;
