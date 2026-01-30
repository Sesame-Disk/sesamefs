import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import toaster from '../toast';

class DismissGroupDialog extends React.Component {

  constructor(props) {
    super(props);
  }

  dismissGroup = () => {
    let that = this;
    seafileAPI.deleteGroup(this.props.groupID).then((res)=> {
      that.props.onGroupChanged();
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
              <h5 className="modal-title">{gettext('Delete Group')}</h5>
              <button type="button" className="close" onClick={this.props.toggleDismissGroupDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <span>{gettext('Really want to delete this group?')}</span>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggleDismissGroupDialog}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.dismissGroup}>{gettext('Delete')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

const DismissGroupDialogPropTypes = {
  showDismissGroupDialog: PropTypes.bool.isRequired,
  toggleDismissGroupDialog: PropTypes.func.isRequired,
  loadGroup: PropTypes.func.isRequired,
  groupID: PropTypes.string,
  onGroupChanged: PropTypes.func.isRequired,
};

DismissGroupDialog.propTypes = DismissGroupDialogPropTypes;

export default DismissGroupDialog;
