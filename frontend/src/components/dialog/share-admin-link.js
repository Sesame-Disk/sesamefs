import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import copy from '../copy-to-clipboard';
import { gettext } from '../../utils/constants';
import toaster from '../../components/toast';

const propTypes = {
  link: PropTypes.string.isRequired,
  toggleDialog: PropTypes.func.isRequired
};

class ShareAdminLink extends React.Component {

  constructor(props) {
    super(props);
  }

  copyToClipboard = () => {
    copy(this.props.link);
    this.props.toggleDialog();
    toaster.success(gettext('The link is copied to the clipboard.'), {duration: 2});
  };

  render() {
    const { link, toggleDialog } = this.props;
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{gettext('Link')}</h5>
              <button type="button" className="close" onClick={toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <a href={link}>{link}</a>
        </div>
        <div className="modal-footer">
          <Button color="primary" onClick={this.copyToClipboard}>{gettext('Copy')}</Button>
          <Button color="secondary" onClick={toggleDialog}>{gettext('Close')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

ShareAdminLink.propTypes = propTypes;

export default ShareAdminLink;
