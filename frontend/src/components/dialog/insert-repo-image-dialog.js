import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { Utils } from '../../utils/utils';
import FileChooser from '../file-chooser/file-chooser';
import '../../css/insert-repo-image-dialog.css';

const { siteRoot, serviceUrl } = window.app.config;
const propTypes = {
  repoID: PropTypes.string.isRequired,
  filePath: PropTypes.string.isRequired,
  toggleCancel: PropTypes.func.isRequired,
};

class InsertRepoImageDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      repo: null,
      selectedPath: '',
    };
  }

  insertImage = () => {
    const url = serviceUrl + '/lib/' + this.state.repo.repo_id + '/file' + Utils.encodePath(this.state.selectedPath) + '?raw=1';
    window.richMarkdownEditor.onInsertImage(url);
    this.props.toggleCancel();
  };

  onDirentItemClick = (repo, selectedPath, dirent) => {
    if (dirent.type === 'file' && Utils.imageCheck(dirent.name)) {
      this.setState({
        repo: repo,
        selectedPath: selectedPath,
      });
    }
    else {
      this.setState({repo: null, selectedPath: ''});
    }
  };

  onRepoItemClick = () => {
    this.setState({repo: null, selectedPath: ''});
  };

  render() {
    const toggle = this.props.toggleCancel;
    const fileSuffixes = ['jpg', 'png', 'jpeg', 'gif', 'bmp'];
    let imageUrl;
    if (this.state.repo) {
      imageUrl = siteRoot + 'thumbnail/' + this.state.repo.repo_id + '/1024' + this.state.selectedPath;
    }
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Select Image')}</h5>
              <button type="button" className="close" onClick={toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <div className="d-flex">
            <div className="col-6">
              <FileChooser
                isShowFile={true}
                repoID={this.props.repoID}
                onDirentItemClick={this.onDirentItemClick}
                onRepoItemClick={this.onRepoItemClick}
                mode="current_repo_and_other_repos"
                fileSuffixes={fileSuffixes}
              />
            </div>
            <div className="insert-image-container col-6">
              {imageUrl ?
                <img src={imageUrl} className='d-inline-block mh-100 mw-100' alt=''/> :
                <span>{gettext('No preview')}</span>
              }
            </div>
          </div>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={toggle}>{gettext('Cancel')}</Button>
          {this.state.selectedPath ?
            <Button color="primary" onClick={this.insertImage}>{gettext('Submit')}</Button>
            : <Button color="primary" disabled>{gettext('Submit')}</Button>
          }
        </div>
      </div>
          </div>
        </div>
    );
  }
}

InsertRepoImageDialog.propTypes = propTypes;

export default InsertRepoImageDialog;
