import React from 'react';
import PropTypes from 'prop-types';
import { Button } from 'reactstrap';
import { gettext } from '../../utils/constants';
import toaster from '../toast';
import copy from '../copy-to-clipboard';

const propTypes = {
  toggle: PropTypes.func.isRequired,
  invitationLink: PropTypes.string.isRequired
};

class OrgAdminInviteUserViaWeiXinDialog extends React.Component {

  constructor(props) {
    super(props);
  }

  copyLink = () => {
    copy(this.props.invitationLink);
    this.props.toggle();
    const message = gettext('Internal link has been copied to clipboard');
    toaster.success(message, {
      duration: 2
    });
  };

  render() {
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
          <div className="modal-dialog modal-dialog-centered">
            <div className="modal-content">
        <div className="modal-header">
              <h5 className="modal-title">{'通过微信邀请用户'}</h5>
              <button type="button" className="close" onClick={this.props.toggle} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
        <div className="modal-body">
          <p>{'请将邀请链接发送给其他人，这样他们就可以通过扫描链接里的二维码来加入组织。'}</p>
          <p>{this.props.invitationLink}</p>
        </div>
        <div className="modal-footer">
          <Button color="primary" onClick={this.copyLink}>{gettext('Copy')}</Button>
        </div>
      </div>
          </div>
        </div>
    );
  }
}

OrgAdminInviteUserViaWeiXinDialog.propTypes = propTypes;

export default OrgAdminInviteUserViaWeiXinDialog;
