import React, { Component } from 'react';
import PropTypes from 'prop-types';
import { seafileAPI } from '../../utils/seafile-api';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { Utils } from '../../utils/utils';

const propTypes = {
  repo: PropTypes.object.isRequired,
  isRepoDeleted: PropTypes.bool.isRequired,
  toggle: PropTypes.func.isRequired,
  onDeleteRepo: PropTypes.func.isRequired,
};

class DeleteRepoDialog extends Component {

  constructor(props) {
    super(props);
    this.state = {
      isRequestSended: false,
      sharedToUserCount: 0,
      sharedToGroupCount: 0,
    };
  }

  UNSAFE_componentWillReceiveProps(nextProps) {
    if (!nextProps.isRepoDeleted) {
      this.setState({ isRequestSended: false });
    }
  }

  componentDidMount() {
    seafileAPI.getRepoFolderShareInfo(this.props.repo.repo_id).then((res) => {
      this.setState({
        sharedToUserCount: res.data['shared_user_emails'].length,
        sharedToGroupCount: res.data['shared_group_ids'].length,
      });
    }).catch(() => {
      // Don't block on share info errors
    });
  }

  onDeleteRepo = () => {
    this.setState({ isRequestSended: true }, () => {
      this.props.onDeleteRepo(this.props.repo);
    });
  };

  render() {

    const { isRequestSended } = this.state;
    const repo = this.props.repo;
    const repoName = '<span class="op-target">' + Utils.HTMLescape(repo.repo_name || repo.name) + '</span>';
    let message = gettext('Are you sure you want to delete %s ?');
    message = message.replace('%s', repoName);

    let alert_message = '';
    if (this.state.sharedToUserCount > 0 || this.state.sharedToGroupCount > 0) {
      alert_message = gettext('This library has been shared to {user_amount} user(s) and {group_amount} group(s).');
      alert_message = alert_message.replace('{user_amount}', this.state.sharedToUserCount);
      alert_message = alert_message.replace('{group_amount}', this.state.sharedToGroupCount);
    }

    const { toggle: toggleDialog } = this.props;

    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
        <div className="modal-dialog modal-dialog-centered">
          <div className="modal-content">
            <div className="modal-header">
              <h5 className="modal-title">{gettext('Delete Library')}</h5>
              <button type="button" className="close" onClick={toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
            <div className="modal-body">
              <p dangerouslySetInnerHTML={{ __html: message }}></p>
              {alert_message !== '' && <p className="error" dangerouslySetInnerHTML={{ __html: alert_message }}></p>}
            </div>
            <div className="modal-footer">
              <Button color="secondary" onClick={toggleDialog}>{gettext('Cancel')}</Button>
              <Button color="primary" disabled={isRequestSended} onClick={this.onDeleteRepo}>{gettext('Delete')}</Button>
            </div>
          </div>
        </div>
      </div>
    );
  }
}

DeleteRepoDialog.propTypes = propTypes;

export default DeleteRepoDialog;
