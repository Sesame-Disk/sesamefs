import React from 'react';
import PropTypes from 'prop-types';
import { Button, Alert } from 'reactstrap';
import { gettext } from '../../utils/constants';
import FileChooser from '../file-chooser/file-chooser';

const propTypes = {
  sharedToken: PropTypes.string.isRequired,
  parentDir: PropTypes.string.isRequired,
  items: PropTypes.array.isRequired,
  toggleCancel: PropTypes.func.isRequired,
  handleSaveSharedDir: PropTypes.func.isRequired,
};

class SaveSharedDirDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      repo: null,
      selectedPath: '',
      errMessage: '',
    };
  }

  onSaveSharedFile = () => {
    this.props.handleSaveSharedDir(this.state.repo.repo_id, this.state.selectedPath);
  };

  onDirentItemClick = (repo, selectedPath, dirent) => {
    if (dirent.type === 'dir') {
      this.setState({
        repo: repo,
        selectedPath: selectedPath,
      });
    }
    else {
      this.setState({
        repo: null,
        selectedPath: '',
      });
    }
  };

  onRepoItemClick = (repo) => {
    this.setState({
      repo: repo,
      selectedPath: '/',
    });
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Save to:')}</h5>
              <button type="button" className="close" onClick={this.props.toggleCancel} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <FileChooser
            isShowFile={false}
            onDirentItemClick={this.onDirentItemClick}
            onRepoItemClick={this.onRepoItemClick}
            mode="only_all_repos"
          />
          {this.state.errMessage && <Alert color="danger">{this.state.errMessage}</Alert>}
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggleCancel}>{gettext('Cancel')}</Button>
          { this.state.selectedPath ?
            <Button color="primary" onClick={this.onSaveSharedFile}>{gettext('Submit')}</Button>
            :
            <Button color="primary" disabled>{gettext('Submit')}</Button>
          }
        </div>
      </div>
          </div>
        </div>
    );
  }
}

SaveSharedDirDialog.propTypes = propTypes;

export default SaveSharedDirDialog;
