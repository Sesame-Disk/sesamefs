import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext, username } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import toaster from '../toast';

class LeaveGroupDialog extends React.Component {

  constructor(props) {
    super(props);
  }

  leaveGroup = () => {
    seafileAPI.quitGroup(this.props.groupID, username).then((res)=> {
      this.props.onGroupChanged();
    }).catch(error => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  render() {
    return(
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Leave Group')}</h5>
              <button type="button" className="close" onClick={this.props.toggleLeaveGroupDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{gettext('Really want to leave this group?')}</p>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggleLeaveGroupDialog}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.leaveGroup}>{gettext('Leave')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

const LeaveGroupDialogPropTypes = {
  toggleLeaveGroupDialog: PropTypes.func.isRequired,
  groupID: PropTypes.string,
  onGroupChanged: PropTypes.func.isRequired,
};

LeaveGroupDialog.propTypes = LeaveGroupDialogPropTypes;

export default LeaveGroupDialog;
