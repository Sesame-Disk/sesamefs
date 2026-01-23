import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import toaster from '../toast';
import copy from '../copy-to-clipboard';
import { gettext } from '../../utils/constants';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import Loading from '../loading';

const propTypes = {
  path: PropTypes.string.isRequired,
  repoID: PropTypes.string.isRequired,
  direntType: PropTypes.string,
  repoEncrypted: PropTypes.bool
};

class InternalLink extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      smartLink: '',
      isInternalLoding: true,
    };
  }

  componentDidMount() {
    // Check if library is encrypted - encrypted libraries cannot be shared
    // Handle both boolean and integer (0/1) values
    console.log('[InternalLink] repoEncrypted value:', this.props.repoEncrypted, 'type:', typeof this.props.repoEncrypted);

    if (this.props.repoEncrypted === true || this.props.repoEncrypted === 1 || this.props.repoEncrypted === '1') {
      console.log('[InternalLink] Library is encrypted, skipping smart link API call');
      this.setState({
        isInternalLoding: false,
        smartLink: ''
      });
      return;
    }

    console.log('[InternalLink] Library not encrypted, calling getInternalLink');
    let { repoID, path, direntType } = this.props;
    seafileAPI.getInternalLink(repoID, path, direntType).then(res => {
      this.setState({
        smartLink: res.data.smart_link,
        isInternalLoding: false
      });
    }).catch(error => {
      // Silently handle 404 errors (endpoint not implemented yet)
      // For other errors, show a message
      if (error.response && error.response.status === 404) {
        // Smart link endpoint not implemented - just show no link available
        this.setState({
          isInternalLoding: false,
          smartLink: ''
        });
      } else {
        let errMessage = Utils.getErrorMsg(error);
        toaster.danger(errMessage);
        this.setState({
          isInternalLoding: false,
          smartLink: ''
        });
      }
    });
  }

  copyToClipBoard = () => {
    copy(this.state.smartLink);
    let message = gettext('Internal link has been copied to clipboard');
    toaster.success(message, {
      duration: 2
    });
  };

  render() {
    if (this.state.isInternalLoding) {
      return (<Loading />);
    }

    // Show policy message for encrypted libraries
    // Handle both boolean and integer (0/1) values
    if (this.props.repoEncrypted === true || this.props.repoEncrypted === 1 || this.props.repoEncrypted === '1') {
      return (
        <div className="alert alert-warning" role="alert">
          <h6 className="alert-heading">{gettext('Cannot share encrypted library')}</h6>
          <p className="mb-0">
            {gettext('Files in password-encrypted libraries cannot be shared. Please move the files to a public library to enable sharing.')}
          </p>
        </div>
      );
    }

    if (!this.state.smartLink) {
      return (
        <div>
          <p className="text-muted">{gettext('Unable to generate internal link.')}</p>
        </div>
      );
    }
    return (
      <div>
        <p className="tip mb-1">
          {gettext('An internal link is a link to a file or folder that can be accessed by users with read permission to the file or folder.')}
        </p>
        <p>
          <a target="_blank" href={this.state.smartLink} rel="noreferrer">{this.state.smartLink}</a>
        </p>
        <Button onClick={this.copyToClipBoard} color="primary" className="mt-2">{gettext('Copy')}</Button>
      </div>
    );
  }
}

InternalLink.propTypes = propTypes;

export default InternalLink;
