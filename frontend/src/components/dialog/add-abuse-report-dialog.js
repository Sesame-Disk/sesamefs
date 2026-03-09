import React from 'react';
import PropTypes from 'prop-types';
import { Button, Form, FormGroup, Label, Input, Alert } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import toaster from '../toast';

const propTypes = {
  sharedToken: PropTypes.string.isRequired,
  filePath: PropTypes.string.isRequired,
  toggleAddAbuseReportDialog: PropTypes.func.isRequired,
  isAddAbuseReportDialogOpen: PropTypes.bool.isRequired,
  contactEmail: PropTypes.string.isRequired,
};

class AddAbuseReportDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      abuseType: 'copyright',
      description: '',
      reporter: this.props.contactEmail,
      errMessage: '',
    };
  }

  onAbuseReport = () => {
    if (!this.state.reporter) {
      this.setState({
        errMessage: gettext('Contact information is required.')
      });
      return;
    }
    seafileAPI.addAbuseReport(this.props.sharedToken, this.state.abuseType, this.state.description, this.state.reporter, this.props.filePath).then((res) => {
      this.props.toggleAddAbuseReportDialog();
      toaster.success(gettext('Success'), {duration: 2});
    }).catch((error) => {
      let errorMsg = Utils.getErrorMsg(error);
      this.setState({ errMessage: errorMsg });
    });
  };

  onAbuseTypeChange = (event) => {
    let type = event.target.value;
    if (type === this.state.abuseType) {
      return;
    }
    this.setState({abuseType: type});
  };

  setReporter = (event) => {
    let reporter = event.target.value.trim();
    this.setState({reporter: reporter});
  };

  setDescription = (event) => {
    let desc = event.target.value.trim();
    this.setState({description: desc});
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Report Abuse')}</h5>
              <button type="button" className="close" onClick={this.props.toggleAddAbuseReportDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <Form>
            <FormGroup>
              <Label for="abuse-type-select">{gettext('Abuse Type')}</Label>
              <Input type="select" id="abuse-type-select" onChange={(event) => this.onAbuseTypeChange(event)}>
                <option value='copyright'>{gettext('Copyright Infringement')}</option>
                <option value='virus'>{gettext('Virus')}</option>
                <option value='abuse_content'>{gettext('Abuse Content')}</option>
                <option value='other'>{gettext('Other')}</option>
              </Input>
            </FormGroup>
            <FormGroup>
              <Label>{gettext('Contact Information')}</Label>
              <Input type="text" value={this.state.reporter} onChange={(event) => this.setReporter(event)}/>
            </FormGroup>
            <FormGroup>
              <Label>{gettext('Description')}</Label>
              <Input type="textarea" onChange={(event) => this.setDescription(event)}/>
            </FormGroup>
          </Form>
          {this.state.errMessage && <Alert color="danger">{this.state.errMessage}</Alert>}
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggleAddAbuseReportDialog}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.onAbuseReport}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

AddAbuseReportDialog.propTypes = propTypes;

export default AddAbuseReportDialog;
