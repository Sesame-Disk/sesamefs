import React from 'react';
import PropTypes from 'prop-types';
import { gettext } from '../../utils/constants';
import { Button } from 'reactstrap';

const propTypes = {
  currentResumableFile: PropTypes.object.isRequired,
  replaceRepetitionFile: PropTypes.func.isRequired,
  uploadFile: PropTypes.func.isRequired,
  cancelFileUpload: PropTypes.func.isRequired,
};

class UploadRemindDialog extends React.Component {

  toggle = (e) => {
    e.nativeEvent.stopImmediatePropagation();
    this.props.cancelFileUpload();
  };

  replaceRepetitionFile = (e) => {
    e.nativeEvent.stopImmediatePropagation();
    this.props.replaceRepetitionFile();
  };

  uploadFile = (e) => {
    e.nativeEvent.stopImmediatePropagation();
    this.props.uploadFile();
  };

  render() {
    const { fileName } = this.props.currentResumableFile;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title"><span>{gettext('Replace file {filename}?').replace('{filename}', fileName)}</span></h5>
              <button type="button" className="close" onClick={this.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{gettext('A file with the same name already exists in this folder.')}</p>
          <p>{gettext('Replacing it will overwrite its content.')}</p>
        </div>
        <div className="modal-footer">
          <Button color="primary" onClick={this.replaceRepetitionFile}>{gettext('Replace')}</Button>
          <Button color="primary" onClick={this.uploadFile}>{gettext('Don\'t replace')}</Button>
          <Button color="secondary" onClick={this.toggle}>{gettext('Cancel')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

UploadRemindDialog.propTypes = propTypes;

export default UploadRemindDialog;
