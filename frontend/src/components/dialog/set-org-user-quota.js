import React from 'react';
import PropTypes from 'prop-types';
import { InputGroup, InputGroupAddon, InputGroupText } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';

const propTypes = {
  orgID: PropTypes.string,
  email: PropTypes.string.isRequired,
  quotaTotal: PropTypes.string.isRequired,
  updateQuota: PropTypes.func.isRequired,
  toggleDialog: PropTypes.func.isRequired
};

class SetOrgUserQuota extends React.Component {

  constructor(props) {
    super(props);
    const initialQuota = this.props.quotaTotal < 0 ? '' :
      this.props.quotaTotal / (1000 * 1000);
    this.state = {
      inputValue: initialQuota,
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
    const quota = this.state.inputValue.trim();

    if (!quota) {
      this.setState({
        formErrorMsg: gettext('It is required.')
      });
      return false;
    }

    this.setState({
      submitBtnDisabled: true
    });

    seafileAPI.orgAdminSetOrgUserQuota(orgID, email, quota).then((res) => {
      this.props.updateQuota(res.data.quota_total);
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
              <h5 className="modal-title">{gettext('Set user quota')}</h5>
              <button type="button" className="close" onClick={this.props.toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <React.Fragment>
            <InputGroup>
              <input type="text" className="form-control" value={inputValue} onChange={this.handleInputChange} />
              <InputGroupAddon addonType="append">
                <InputGroupText>MB</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
            <p className="small text-secondary mt-2 mb-2">{gettext('Tip: 0 means default limit')}</p>
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

SetOrgUserQuota.propTypes = propTypes;

export default SetOrgUserQuota;
