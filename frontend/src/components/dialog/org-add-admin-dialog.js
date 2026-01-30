import React from 'react';
import PropTypes from 'prop-types';
import { Button, Alert } from 'reactstrap';
import { gettext, orgID } from '../../utils/constants';
import { Utils } from '../../utils/utils';
import toaster from '../toast';
import UserSelect from '../user-select';
import { seafileAPI } from '../../utils/seafile-api';
import OrgUserInfo from '../../models/org-user';

const propTypes = {
  toggle: PropTypes.func.isRequired,
  onAddedOrgAdmin: PropTypes.func.isRequired,
};

class AddOrgAdminDialog extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      selectedOption: null,
      errMessage: '',
    };
    this.options = [];
  }

  handleSelectChange = (option) => {
    this.setState({
      selectedOption: option,
      errMessage: ''
    });
    this.options = [];
  };

  addOrgAdmin = () => {
    if (!this.state.selectedOption) return;
    const userEmail = this.state.selectedOption.email;
    seafileAPI.orgAdminSetOrgAdmin(orgID, userEmail, true).then(res => {
      let userInfo = new OrgUserInfo(res.data);
      this.props.onAddedOrgAdmin(userInfo);
    }).catch(error => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  toggle = () => {
    this.props.toggle();
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Add Admins')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <UserSelect
            ref="userSelect"
            isMulti={false}
            className="reviewer-select"
            placeholder={gettext('Select a user as admin')}
            onSelectChange={this.handleSelectChange}
          />
          {this.state.errMessage && <Alert color="danger" className="mt-2">{this.state.errMessage}</Alert>}
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle}>{gettext('Close')}</Button>
          <Button color="primary" onClick={this.addOrgAdmin}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

AddOrgAdminDialog.propTypes = propTypes;

export default AddOrgAdminDialog;
