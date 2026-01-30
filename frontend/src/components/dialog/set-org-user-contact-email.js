import React from 'react';
import PropTypes from 'prop-types';

import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';

const propTypes = {
  orgID: PropTypes.string,
  email: PropTypes.string.isRequired,
  contactEmail: PropTypes.string.isRequired,
  updateContactEmail: PropTypes.func.isRequired,
  toggleDialog: PropTypes.func.isRequired
};

class SetOrgUserContactEmail extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      inputValue: this.props.contactEmail,
      submitBtnDisabled: false
    };
  }

  handleInputChange = (e) => {
    this.setState({
      inputValue: e.target.value
    });
  };

  formSubmit = () => {
    const { orgID, email } = this.props;
    const contactEmail = this.state.inputValue.trim();

    this.setState({
      submitBtnDisabled: true
    });

    seafileAPI.orgAdminSetOrgUserContactEmail(orgID, email, contactEmail).then((res) => {
      const newContactEmail = contactEmail ? res.data.contact_email : '';
      this.props.updateContactEmail(newContactEmail);
      this.props.toggleDialog();
    }).catch((error) => {
      let errorMsg = Utils.getErrorMsg(error);
      this.setState({
        formErrorMsg: errorMsg,
        submitBtnDisabled: false
      });
    });
  };

  render() {
    const { inputValue, formErrorMsg, submitBtnDisabled } = this.state;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Set user contact email')}</h5>
              <button type="button" className="close" onClick={this.props.toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <React.Fragment>
            <input type="text" className="form-control" value={inputValue} onChange={this.handleInputChange} />
            {formErrorMsg && <p className="error m-0 mt-2">{formErrorMsg}</p>}
          </React.Fragment>
        </div>
        <div className="modal-footer">
          <button className="btn btn-secondary" onClick={this.props.toggleDialog}>{gettext('Cancel')}</button>
          <button className="btn btn-primary" disabled={submitBtnDisabled} onClick={this.formSubmit}>{gettext('Submit')}</button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

SetOrgUserContactEmail.propTypes = propTypes;

export default SetOrgUserContactEmail;
