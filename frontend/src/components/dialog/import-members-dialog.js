import React from 'react';
import PropTypes from 'prop-types';
import { Alert, Button } from 'reactstrap';
import { gettext, siteRoot, groupImportMembersExtraMsg } from '../../utils/constants';

const propTypes = {
  toggleImportMembersDialog: PropTypes.func.isRequired,
  importMembersInBatch: PropTypes.func.isRequired,
};

class ImportMembersDialog extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      errorMsg: ''
    };
    this.fileInputRef = React.createRef();
  }

  toggle = () => {
    this.props.toggleImportMembersDialog();
  };

  openFileInput = () => {
    this.fileInputRef.current.click();
  };

  uploadFile = (e) => {
    // no file selected
    if (!this.fileInputRef.current.files.length) {
      return;
    }
    // check file extension
    let fileName = this.fileInputRef.current.files[0].name;
    if(fileName.substr(fileName.lastIndexOf('.') + 1) != 'xlsx') {
      this.setState({
        errorMsg: gettext('Please choose a .xlsx file.')
      });
      return;
    }
    const file = this.fileInputRef.current.files[0];
    this.props.importMembersInBatch(file);
    this.toggle();
  };

  render() {
    let { errorMsg } = this.state;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Import members from a .xlsx file')}</h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>

        <div className="modal-body">
          <p>{groupImportMembersExtraMsg}</p>
          <p><a className="text-secondary small" href={`${siteRoot}api/v2.1/group-members-import-example/`}>{gettext('Download an example file')}</a></p>
          <button className="btn btn-outline-primary" onClick={this.openFileInput}>{gettext('Upload file')}</button>
          <input className="d-none" type="file" onChange={this.uploadFile} ref={this.fileInputRef} />
          {errorMsg && <Alert color="danger">{errorMsg}</Alert>}
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.toggle}>{gettext('Cancel')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ImportMembersDialog.propTypes = propTypes;

export default ImportMembersDialog;
