import React from 'react';
import PropTypes from 'prop-types';
import { Button, FormGroup, Label, Input, Alert } from 'reactstrap';
import { gettext, siteRoot } from '../../../utils/constants';
import moment from 'moment';

class LogsExportExcelDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      loading: true,
      startDateStr: '',
      endDateStr: '',
      errMsg: '',
    };
  }

  downloadExcel = () => {
    if (!this.isValidDateStr()) {
      return;
    }
    let { startDateStr, endDateStr } = this.state;
    let url = siteRoot;

    switch (this.props.logType) {
      case 'login':
        url += 'sys/loginadmin/export-excel/';
        break;
      case 'fileAccess':
        url += 'sys/log/fileaudit/export-excel/';
        break;
      case 'fileUpdate':
        url += 'sys/log/fileupdate/export-excel/';
        break;
      case 'sharePermission':
        url += 'sys/log/permaudit/export-excel/';
        break;
    }
    location.href = url + '?start=' + startDateStr + '&end=' + endDateStr;
    this.props.toggle();
  };

  isValidDateStr = () => {
    let { startDateStr, endDateStr } = this.state;
    if (moment(startDateStr, 'YYYY-MM-DD', true).isValid() &&
      moment(endDateStr, 'YYYY-MM-DD', true).isValid() &&
      moment(startDateStr).isBefore(endDateStr)
    ) {
      return true;
    } else {
      this.setState({
        errMsg: gettext('Date Invalid.')
      });
      return false;
    }
  };

  handleStartChange = (e) => {
    const startDateStr = e.target.value.trim();
    this.setState({
      startDateStr: startDateStr,
      errMsg: ''
    });
  };

  handleEndChange = (e) => {
    const endDateStr = e.target.value.trim();
    this.setState({
      endDateStr: endDateStr,
      errMsg: '',
    });
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Choose date')}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <FormGroup>
            <Label>{gettext('Start date')}</Label>
            <Input
              value={this.state.startDateStr}
              onChange={this.handleStartChange}
              placeholder='yyyy-mm-dd'
              autoFocus={true}
            />
          </FormGroup>
          <FormGroup>
            <Label>{gettext('End date')}</Label>
            <Input
              value={this.state.endDateStr}
              onChange={this.handleEndChange}
              placeholder='yyyy-mm-dd'
            />
          </FormGroup>
          {this.state.errMsg &&
            <Alert className="mt-2" color="danger">
              {gettext(this.state.errMsg)}
            </Alert>
          }
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggle}>{gettext('Cancel')}</Button>
          <Button color="primary" onClick={this.downloadExcel}>{gettext('Submit')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

const propTypes = {
  toggle: PropTypes.func.isRequired,
  logType: PropTypes.string.isRequired,
};

LogsExportExcelDialog.propTypes = propTypes;

export default LogsExportExcelDialog;
