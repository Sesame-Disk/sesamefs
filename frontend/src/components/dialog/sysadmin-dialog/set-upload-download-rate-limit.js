import React from 'react';
import PropTypes from 'prop-types';
import { Button, Form, FormGroup, Input, InputGroup, InputGroupAddon, InputGroupText } from 'reactstrap';
import { gettext } from '../../../utils/constants';

const propTypes = {
  uploadOrDownload: PropTypes.string.isRequired,
  toggle: PropTypes.func.isRequired,
  updateUploadDownloadRateLimit: PropTypes.func.isRequired
};

class SysAdminSetUploadDownloadRateLimitDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      rateLimit: '',
      isSubmitBtnActive: false
    };
  }

  toggle = () => {
    this.props.toggle();
  };

  handleRateLimitChange = (e) => {
    const value = e.target.value;
    this.setState({
      rateLimit: value,
      isSubmitBtnActive: value.trim() != ''
    });
  };

  handleKeyDown = (e) => {
    if (e.key == 'Enter') {
      this.handleSubmit();
      e.preventDefault();
    }
  };

  handleSubmit = () => {
    this.props.updateUploadDownloadRateLimit(this.props.uploadOrDownload, this.state.rateLimit.trim());
    this.toggle();
  };

  render() {
    const { rateLimit, isSubmitBtnActive } = this.state;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{this.props.uploadOrDownload == 'upload' ? gettext('Set Upload Rate Limit') : gettext('Set Download Rate Limit')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <Form>
            <FormGroup>
              <InputGroup>
                <Input
                  type="text"
                  className="form-control"
                  value={rateLimit}
                  onKeyDown={this.handleKeyDown}
                  onChange={this.handleRateLimitChange}
                />
                <InputGroupAddon addonType="append">
                  <InputGroupText>kB/s</InputGroupText>
                </InputGroupAddon>
              </InputGroup>
              <p className="small text-secondary mt-2 mb-2">
                {gettext('An integer that is greater than or equal to 0.')}
                <br />
                {gettext('Tip: 0 means default limit')}
              </p>
            </FormGroup>
          </Form>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.handleSubmit} disabled={!isSubmitBtnActive}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

SysAdminSetUploadDownloadRateLimitDialog.propTypes = propTypes;

export default SysAdminSetUploadDownloadRateLimitDialog;
