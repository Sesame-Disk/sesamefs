import React, { Component } from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';
import { Utils } from '../../utils/utils';

const propTypes = {
  repos: PropTypes.array.isRequired,
  toggle: PropTypes.func.isRequired,
  onDeleteRepos: PropTypes.func.isRequired,
};

class BatchDeleteRepoDialog extends Component {

  constructor(props) {
    super(props);
    this.state = {
      isDeleting: false,
    };
  }

  onDeleteRepos = () => {
    this.setState({ isDeleting: true });
    this.props.onDeleteRepos(this.props.repos);
  };

  render() {
    const { isDeleting } = this.state;
    const { repos, toggle: toggleDialog } = this.props;
    const count = repos.length;

    let title, message;
    if (count === 1) {
      const repoName = '<span class="op-target">' + Utils.HTMLescape(repos[0].repo_name) + '</span>';
      title = gettext('Delete Library');
      message = gettext('Are you sure you want to delete %s ?').replace('%s', repoName);
    } else {
      title = gettext('Delete Libraries');
      message = gettext('Are you sure you want to delete {count} libraries?').replace('{count}', count);
    }

    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
        <div className="modal-dialog modal-dialog-centered">
          <div className="modal-content">
            <div className="modal-header">
              <h5 className="modal-title">{title}</h5>
              <button type="button" className="close" onClick={toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
            <div className="modal-body">
              <p dangerouslySetInnerHTML={{__html: message}}></p>
              {count > 1 && (
                <ul className="list-unstyled" style={{ maxHeight: '200px', overflowY: 'auto' }}>
                  {repos.map(repo => (
                    <li key={repo.repo_id} className="text-truncate">
                      <i className="sf2-icon-library mr-2"></i>
                      {repo.repo_name}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div className="modal-footer">
              <Button color="secondary" onClick={toggleDialog}>{gettext('Cancel')}</Button>
              <Button color="primary" disabled={isDeleting} onClick={this.onDeleteRepos}>
                {isDeleting ? gettext('Deleting...') : gettext('Delete')}
              </Button>
            </div>
          </div>
        </div>
      </div>
    );
  }
}

BatchDeleteRepoDialog.propTypes = propTypes;

export default BatchDeleteRepoDialog;
