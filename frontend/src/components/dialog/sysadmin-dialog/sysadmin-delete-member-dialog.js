import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../../utils/constants';
import { seafileAPI } from '../../../utils/seafile-api';
import { Utils } from '../../../utils/utils';
import toaster from '../../toast';

const propTypes = {
  member: PropTypes.object.isRequired,
  groupID: PropTypes.string,
  toggle: PropTypes.func.isRequired,
  onMemberChanged: PropTypes.func.isRequired
};

class DeleteMemberDialog extends React.Component {

  constructor(props) {
    super(props);
  }

  deleteMember = () => {
    const userEmail = this.props.member.email;
    seafileAPI.sysAdminDeleteGroupMember(this.props.groupID, userEmail).then((res) => {
      if (res.data.success) {
        this.props.onMemberChanged();
        this.props.toggle();
      }
    }).catch(error => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  render() {
    let tipMessage = gettext('Are you sure you want to delete {placeholder} ?');
    tipMessage = tipMessage.replace('{placeholder}', '<span class="op-target">' + Utils.HTMLescape(this.props.member.name) + '</span>');
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Delete Member')}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <div dangerouslySetInnerHTML={{__html: tipMessage}}></div>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.deleteMember}>{gettext('Delete')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

DeleteMemberDialog.propTypes = propTypes;

export default DeleteMemberDialog;
