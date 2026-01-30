import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import toaster from '../toast';
import copy from '../copy-to-clipboard';
import { gettext } from '../../utils/constants';

const propTypes = {
  currentLinkHref: PropTypes.string.isRequired,
  toggle: PropTypes.func.isRequired,
};

class ViewLinkDialog extends React.Component {

  constructor(props) {
    super(props);
  }

  copyToClipBoard = () => {
    copy(this.props.currentLinkHref);
    let message = gettext('Link has been copied to clipboard');
    toaster.success(message, {
      duration: 2
    });
    this.props.toggle();
  };

  render() {
    const href = this.props.currentLinkHref;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Link')}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p><a target="_blank" href={href} rel="noreferrer">{href}</a></p>
        </div>
        <div className="modal-footer">
          <Button color="secondary" onClick={this.props.toggle}>{gettext('Cancel')}</Button>{' '}
          <Button color="primary" onClick={this.copyToClipBoard}>{gettext('Copy')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ViewLinkDialog.propTypes = propTypes;

export default ViewLinkDialog;
