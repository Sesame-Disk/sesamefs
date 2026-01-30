import React from 'react';
import PropTypes from 'prop-types';

import { gettext } from '../../utils/constants';
import ListAndAddGroupMembers from '../list-and-add-group-members';

import '../../css/manage-members-dialog.css';

const propTypes = {
  groupID: PropTypes.string,
  isOwner: PropTypes.bool.isRequired,
  toggleManageMembersDialog: PropTypes.func.isRequired
};

class ManageMembersDialog extends React.Component {

  render() {
    const { groupID, isOwner, toggleManageMembersDialog: toggle } = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Manage group members')}</h5>
              <button type="button" className="close" onClick={toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body pb-0">
          <ListAndAddGroupMembers
            groupID={groupID}
            isOwner={isOwner}
          />
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ManageMembersDialog.propTypes = propTypes;

export default ManageMembersDialog;
