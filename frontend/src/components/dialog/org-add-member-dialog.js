import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext, orgID } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import toaster from '../toast';
import UserSelect from '../user-select';

const propTypes = {
  toggle: PropTypes.func.isRequired,
  groupID:  PropTypes.string.isRequired,
  onMemberChanged: PropTypes.func.isRequired
};

class AddMemberDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      selectedOption: null,
      errMessage: '',
    };
    this.Options = [];
  }

  handleSelectChange = (option) => {
    this.setState({ selectedOption: option });
    this.Options = [];
  };

  handleSubmit = () => {
    if (!this.state.selectedOption) return;
    const email = this.state.selectedOption.email;
    this.refs.orgSelect.clearSelect();
    this.setState({ errMessage: [] });
    seafileAPI.orgAdminAddGroupMember(orgID, this.props.groupID, email).then((res) => {
      this.setState({ selectedOption: null });
      if (res.data.failed.length > 0) {
        this.setState({ errMessage: res.data.failed[0].error_msg });
      }
      if (res.data.success.length > 0) {
        this.props.onMemberChanged();
        this.props.toggle();
      }
    }).catch(error => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Add Member')}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <UserSelect
            placeholder={gettext('Search users')}
            onSelectChange={this.handleSelectChange}
            ref="orgSelect"
            isMulti={false}
            className='org-add-member-select'
          />
          { this.state.errMessage && <p className="error">{this.state.errMessage}</p> }
        </div>
        <div className="modal-footer">
          <Button color="primary" onClick={this.handleSubmit}>{gettext('Submit')}</Button>
          <Button color="secondary" onClick={this.props.toggle}>{gettext('Cancel')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

AddMemberDialog.propTypes = propTypes;

export default AddMemberDialog;
