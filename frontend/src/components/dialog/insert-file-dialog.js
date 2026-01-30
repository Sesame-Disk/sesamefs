import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';
import FileChooser from '../file-chooser/file-chooser';

const propTypes = {
  repoID: PropTypes.string.isRequired,
  filePath: PropTypes.string.isRequired,
  toggleCancel: PropTypes.func.isRequired,
  getInsertLink: PropTypes.func.isRequired,
};

class InsertFileDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      repo: null,
      selectedPath: '',
    };
  }

  handleInsert = () => {
    this.props.getInsertLink(this.state.repo.repo_id, this.state.selectedPath);
    this.props.toggleCancel();
  };

  onDirentItemClick = (repo, selectedPath, dirent) => {
    if (dirent.type === 'file') {
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

  onRepoItemClick = () => {
    this.setState({
      repo: null,
      selectedPath: '',
    });
  };

  render() {
    const toggle = this.props.toggleCancel;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Select File')}</h5>
              <button type="button" className="close" onClick={toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <FileChooser
            isShowFile={true}
            repoID={this.props.repoID}
            onDirentItemClick={this.onDirentItemClick}
            onRepoItemClick={this.onRepoItemClick}
            mode="current_repo_and_other_repos"
          />
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={toggle}>{gettext('Cancel')}</Button>
          {this.state.selectedPath ? <Button color="primary" onClick={this.handleInsert}>{gettext('Submit')}</Button>
            : <Button color="primary" disabled>{gettext('Submit')}</Button>}
        </div>
      </div>
          </div>
        </div>
    );
  }
}

InsertFileDialog.propTypes = propTypes;

export default InsertFileDialog;
