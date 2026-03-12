import React, { Fragment } from 'react';
import PropTypes from 'prop-types';
import moment from 'moment';
import copy from 'copy-to-clipboard';
import { Button, Input, InputGroup, InputGroupAddon, FormGroup, Label, Alert } from 'reactstrap';
import { isPro, gettext, shareLinkExpireDaysMin, shareLinkExpireDaysMax, shareLinkExpireDaysDefault, canSendShareLinkEmail, shareLinkPasswordMinLength, shareLinkPasswordStrengthLevel } from '../../utils/constants';
import Selector from '../../components/single-selector';
import CommonOperationConfirmationDialog from '../../components/dialog/common-operation-confirmation-dialog';
import { seafileAPI } from '../../utils/seafile-api';
import { Utils } from '../../utils/utils';
import ShareLink from '../../models/share-link';
import toaster from '../toast';
import SendLink from '../send-link';
import SharedLink from '../shared-link';
import SetLinkExpiration from '../set-link-expiration';
import { EvalProFunc } from '../../services/ad';

const propTypes = {
  sharedLinkInfo: PropTypes.object.isRequired,
  permissionOptions: PropTypes.array.isRequired,
  defaultExpireDays: PropTypes.oneOfType([
    PropTypes.string,
    PropTypes.number
  ]),
  showLinkDetails: PropTypes.func.isRequired,
  updateLink: PropTypes.func.isRequired,
  deleteLink: PropTypes.func.isRequired,
  closeShareDialog: PropTypes.func.isRequired
};

class LinkDetails extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      storedPasswordVisible: false,
      isEditingExpiration: false,
      isExpirationEditIconShow: false,
      expType: 'by-days',
      expireDays: this.props.defaultExpireDays,
      expDate: null,
      isOpIconShown: false,
      isLinkDeleteDialogOpen: false,
      isSendLinkShown: false,
      isChangingPassword: false,
      newPassword: '',
      newPasswordConfirm: '',
      newPasswordVisible: false,
      passwordChangeError: '',
      isRemovePasswordDialogOpen: false,
    };
  }

  onCopySharedLink = () => {
    const { sharedLinkInfo } = this.props;
    copy(sharedLinkInfo.link);
    toaster.success(gettext('Share link is copied to the clipboard.'));
  };

  onCopyPassword = () => {
    const { sharedLinkInfo } = this.props;
    copy(sharedLinkInfo.password);
    toaster.success(gettext('Password is copied to the clipboard.'));
  };

  onCopyLinkAndPassword = () => {
    const { sharedLinkInfo } = this.props;
    const textToCopy = `Share Link: ${sharedLinkInfo.link}\nPassword: ${sharedLinkInfo.password}\n\nOpen the link and enter the password to access the files.`;
    copy(textToCopy);
    toaster.success(gettext('Link and password copied to the clipboard.'));
  };

  onCopyDownloadLink = () => {
    const { sharedLinkInfo } = this.props;
    copy(`${sharedLinkInfo.link}?dl=1`);
    toaster.success(gettext('Direct download link is copied to the clipboard.'));
  };

  toggleStoredPasswordVisible = () => {
    this.setState({
      storedPasswordVisible: !this.state.storedPasswordVisible
    });
  };

  handleMouseOverExpirationEditIcon = () => {
    this.setState({ isExpirationEditIconShow: true });
  };

  handleMouseOutExpirationEditIcon = () => {
    this.setState({ isExpirationEditIconShow: false });
  };

  editingExpirationToggle = () => {
    this.setState({ isEditingExpiration: !this.state.isEditingExpiration });
  };

  setExpType = (e) => {
    this.setState({
      expType: e.target.value
    });
  };

  onExpDateChanged = (value) => {
    this.setState({
      expDate: value
    });
  };

  onExpireDaysChanged = (e) => {
    let day = e.target.value.trim();
    this.setState({ expireDays: day });
  };

  updateExpiration = () => {
    const { sharedLinkInfo } = this.props;
    const { expType, expireDays, expDate } = this.state;
    let expirationTime = '';
    if (expType === 'by-days') {
      expirationTime = moment().add(parseInt(expireDays), 'days').format();
    } else {
      expirationTime = expDate.format();
    }
    seafileAPI.updateShareLink(sharedLinkInfo.token, '', expirationTime).then((res) => {
      this.setState({
        isEditingExpiration: false
      });
      this.props.updateLink(new ShareLink(res.data));
    }).catch((error) => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  handleMouseOver = () => {
    this.setState({ isOpIconShown: true });
  };

  handleMouseOut = () => {
    this.setState({ isOpIconShown: false });
  };

  changePerm = (permOption) => {
    const { sharedLinkInfo } = this.props;
    const { permissionDetails } = Utils.getShareLinkPermissionObject(permOption.value);
    seafileAPI.updateShareLink(sharedLinkInfo.token, JSON.stringify(permissionDetails)).then((res) => {
      this.props.updateLink(new ShareLink(res.data));
    }).catch((error) => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  toggleLinkDeleteDialog = () => {
    this.setState({ isLinkDeleteDialogOpen: !this.state.isLinkDeleteDialogOpen });
  };

  toggleSendLink = () => {
    this.setState({ isSendLinkShown: !this.state.isSendLinkShown });
  };

  deleteLink = () => {
    const { sharedLinkInfo } = this.props;
    const { token } = sharedLinkInfo;
    this.props.deleteLink(token);
  };

  goBack = () => {
    this.props.showLinkDetails(null);
  };

  toggleChangePassword = () => {
    this.setState({
      isChangingPassword: !this.state.isChangingPassword,
      newPassword: '',
      newPasswordConfirm: '',
      newPasswordVisible: false,
      passwordChangeError: '',
      isRemovePasswordDialogOpen: false,
    });
  };

  toggleNewPasswordVisible = () => {
    this.setState({ newPasswordVisible: !this.state.newPasswordVisible });
  };

  generateNewPassword = () => {
    const val = Utils.generatePassword(shareLinkPasswordMinLength);
    this.setState({ newPassword: val, newPasswordConfirm: val, passwordChangeError: '' });
  };

  submitPasswordChange = () => {
    const { newPassword, newPasswordConfirm } = this.state;
    const { sharedLinkInfo } = this.props;
    if (newPassword.length < shareLinkPasswordMinLength) {
      this.setState({ passwordChangeError: gettext('The password is too short.') });
      return;
    }
    if (newPassword !== newPasswordConfirm) {
      this.setState({ passwordChangeError: gettext('Passwords don\'t match') });
      return;
    }
    if (Utils.getStrengthLevel(newPassword) < shareLinkPasswordStrengthLevel) {
      this.setState({ passwordChangeError: gettext('The password is too weak. It should include at least {passwordStrengthLevel} of the following: number, upper letter, lower letter and other symbols.').replace('{passwordStrengthLevel}', shareLinkPasswordStrengthLevel) });
      return;
    }
    seafileAPI.updateShareLinkPassword(sharedLinkInfo.token, newPassword).then((res) => {
      this.setState({ isChangingPassword: false, newPassword: '', newPasswordConfirm: '', newPasswordVisible: false, passwordChangeError: '' });
      this.props.updateLink(new ShareLink(res.data));
      toaster.success(gettext('Password updated. Save the new password — it won\'t be shown again.'));
    }).catch((error) => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  submitRemovePassword = () => {
    const { sharedLinkInfo } = this.props;
    seafileAPI.updateShareLinkPassword(sharedLinkInfo.token, null).then((res) => {
      this.setState({ isChangingPassword: false, isRemovePasswordDialogOpen: false });
      this.props.updateLink(new ShareLink(res.data));
      toaster.success(gettext('Password removed.'));
    }).catch((error) => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  render() {
    const { sharedLinkInfo, permissionOptions } = this.props;
    const { isOpIconShown } = this.state;
    const currentPermission = Utils.getShareLinkPermissionStr(sharedLinkInfo.permissions);
    this.permOptions = permissionOptions.map(item => {
      return {
        value: item,
        text: Utils.getShareLinkPermissionObject(item).text,
        isSelected: item === currentPermission
      };
    });
    const currentSelectedPermOption = this.permOptions.filter(item => item.isSelected)[0];

    return (
      <div>
        <button className="fa fa-arrow-left back-icon border-0 bg-transparent text-secondary p-0" onClick={this.goBack} title={gettext('Back')} aria-label={gettext('Back')}></button>
        <dl>
          <dt className="text-secondary font-weight-normal">{gettext('Link:')}</dt>
          <dd>
            <SharedLink
              link={sharedLinkInfo.link}
              linkExpired={sharedLinkInfo.is_expired}
              copyLink={this.onCopySharedLink}
            />
          </dd>
          {!sharedLinkInfo.is_dir && sharedLinkInfo.permissions.can_download && ( // just for file
            <>
              <dt className="text-secondary font-weight-normal">{gettext('Direct Download Link:')}</dt>
              <dd>
                <SharedLink
                  link={`${sharedLinkInfo.link}?dl=1`}
                  linkExpired={sharedLinkInfo.is_expired}
                  copyLink={this.onCopyDownloadLink}
                />
              </dd>
            </>
          )}
          {sharedLinkInfo.password && (
            <>
              <dt className="text-secondary font-weight-normal">{gettext('Password:')}</dt>
              <dd className="d-flex align-items-center">
                <span className="mr-2">{this.state.storedPasswordVisible ? sharedLinkInfo.password : '***************'}</span>
                <span
                  tabIndex="0"
                  role="button"
                  aria-label={this.state.storedPasswordVisible ? gettext('Hide') : gettext('Show')}
                  onKeyDown={this.onIconKeyDown}
                  onClick={this.toggleStoredPasswordVisible}
                  className={`eye-icon fas ${this.state.storedPasswordVisible ? 'fa-eye' : 'fa-eye-slash'} mr-2`}
                  title={this.state.storedPasswordVisible ? gettext('Hide') : gettext('Show')}
                ></span>
                <Button
                  color="secondary"
                  size="sm"
                  onClick={this.onCopyPassword}
                  title={gettext('Copy password')}
                >
                  {gettext('Copy')}
                </Button>
              </dd>
              <dd>
                <div className="alert alert-warning py-2 px-3 mb-2" style={{ fontSize: '0.85em' }}>
                  ⚠️ {gettext('Save this password now — it won\'t be shown again after you close this panel.')}
                </div>
              </dd>
              <dd className="mt-1">
                <Button
                  color="primary"
                  size="sm"
                  onClick={this.onCopyLinkAndPassword}
                  className="china-copy-all-btn"
                >
                  📋 {gettext('Copy Link & Password')}
                </Button>
                <small className="d-block mt-1 text-muted">
                  {gettext('Copies both link and password in a shareable format')}
                </small>
              </dd>
            </>
          )}
          {(!sharedLinkInfo.password && sharedLinkInfo.has_password) && (
            <dd>
              <span className="text-muted" style={{ fontSize: '0.85em' }}>🔒 {gettext('This link is password protected.')}</span>
            </dd>
          )}
          {sharedLinkInfo.has_password && (
            <>
              {!this.state.isChangingPassword && (
                <dd className="mt-1">
                  <button className="btn btn-sm btn-outline-secondary mr-2" onClick={this.toggleChangePassword}>
                    {gettext('Set new password')}
                  </button>
                  <button className="btn btn-sm btn-outline-danger" onClick={() => this.setState({ isRemovePasswordDialogOpen: true })}>
                    {gettext('Remove password')}
                  </button>
                </dd>
              )}
              {this.state.isChangingPassword && (
                <dd className="mt-2">
                  <div className="ml-4">
                    <FormGroup>
                      <Label for="passwd-new">{gettext('Password')}</Label>
                      <span className="tip ml-1" style={{ fontSize: '0.8em', color: '#6c757d' }}>
                        {gettext('(at least {passwordMinLength} characters and includes {passwordStrengthLevel} of the following: number, upper letter, lower letter and other symbols)')
                          .replace('{passwordMinLength}', shareLinkPasswordMinLength)
                          .replace('{passwordStrengthLevel}', shareLinkPasswordStrengthLevel)}
                      </span>
                      <InputGroup style={{ width: 250 }}>
                        <Input
                          id="passwd-new"
                          type={this.state.newPasswordVisible ? 'text' : 'password'}
                          value={this.state.newPassword}
                          onChange={(e) => this.setState({ newPassword: e.target.value, passwordChangeError: '' })}
                        />
                        <InputGroupAddon addonType="append">
                          <Button onClick={this.toggleNewPasswordVisible}>
                            <i className={`link-operation-icon fas ${this.state.newPasswordVisible ? 'fa-eye' : 'fa-eye-slash'}`}></i>
                          </Button>
                          <Button onClick={this.generateNewPassword}>
                            <i className="link-operation-icon fas fa-magic"></i>
                          </Button>
                        </InputGroupAddon>
                      </InputGroup>
                    </FormGroup>
                    <FormGroup>
                      <Label for="passwd-new-again">{gettext('Password again')}</Label>
                      <Input
                        id="passwd-new-again"
                        style={{ width: 250 }}
                        type={this.state.newPasswordVisible ? 'text' : 'password'}
                        value={this.state.newPasswordConfirm}
                        onChange={(e) => this.setState({ newPasswordConfirm: e.target.value, passwordChangeError: '' })}
                      />
                    </FormGroup>
                    {this.state.passwordChangeError && (
                      <Alert color="danger" className="mt-1 py-1 px-2" style={{ fontSize: '0.85em' }}>{this.state.passwordChangeError}</Alert>
                    )}
                    <div className="mt-2">
                      <Button color="primary" size="sm" className="mr-2" onClick={this.submitPasswordChange}>{gettext('Update')}</Button>
                      <Button color="secondary" size="sm" onClick={this.toggleChangePassword}>{gettext('Cancel')}</Button>
                    </div>
                  </div>
                </dd>
              )}
              {this.state.isRemovePasswordDialogOpen && (
                <CommonOperationConfirmationDialog
                  title={gettext('Remove password')}
                  message={gettext('Are you sure you want to remove the password? The link will become public.')}
                  executeOperation={this.submitRemovePassword}
                  confirmBtnText={gettext('Remove')}
                  toggleDialog={() => this.setState({ isRemovePasswordDialogOpen: false })}
                />
              )}
            </>
          )}
          {sharedLinkInfo.expire_date && (
            <>
              <dt className="text-secondary font-weight-normal">{gettext('Expiration Date:')}</dt>
              {!this.state.isEditingExpiration &&
                <dd style={{ width: '250px' }} onMouseEnter={this.handleMouseOverExpirationEditIcon} onMouseLeave={this.handleMouseOutExpirationEditIcon}>
                  {moment(sharedLinkInfo.expire_date).format('YYYY-MM-DD HH:mm:ss')}
                  {this.state.isExpirationEditIconShow && (
                    <a href="#"
                      role="button"
                      aria-label={gettext('Edit')}
                      title={gettext('Edit')}
                      className="fa fa-pencil-alt attr-action-icon"
                      onClick={EvalProFunc(this.editingExpirationToggle)}>
                    </a>
                  )}
                </dd>
              }
              {this.state.isEditingExpiration &&
                <dd>
                  <div className="ml-4">
                    <SetLinkExpiration
                      minDays={shareLinkExpireDaysMin}
                      maxDays={shareLinkExpireDaysMax}
                      defaultDays={shareLinkExpireDaysDefault}
                      expType={this.state.expType}
                      setExpType={this.setExpType}
                      expireDays={this.state.expireDays}
                      onExpireDaysChanged={this.onExpireDaysChanged}
                      expDate={this.state.expDate}
                      onExpDateChanged={this.onExpDateChanged}
                    />
                    <div className={this.state.expType === 'by-days' ? 'mt-2' : 'mt-3'}>
                      <button className="btn btn-primary mr-2" onClick={this.updateExpiration}>{gettext('Update')}</button>
                      <button className="btn btn-secondary" onClick={this.editingExpirationToggle}>{gettext('Cancel')}</button>
                    </div>
                  </div>
                </dd>
              }
            </>
          )}
          {(isPro && sharedLinkInfo.permissions) && (
            <>
              <dt className="text-secondary font-weight-normal">{gettext('Permission:')}</dt>
              <dd style={{ width: '250px' }} onMouseEnter={this.handleMouseOver} onMouseLeave={this.handleMouseOut}>
                <Selector
                  isDropdownToggleShown={isOpIconShown && !sharedLinkInfo.is_expired}
                  currentSelectedOption={currentSelectedPermOption}
                  options={this.permOptions}
                  selectOption={this.changePerm}
                />
              </dd>
            </>
          )}
        </dl>
        {(canSendShareLinkEmail && !this.state.isSendLinkShown) &&
          <Button onClick={this.toggleSendLink} className='mr-2'>{gettext('Send')}</Button>
        }
        {this.state.isSendLinkShown &&
          <SendLink
            linkType='shareLink'
            token={sharedLinkInfo.token}
            toggleSendLink={this.toggleSendLink}
            closeShareDialog={this.props.closeShareDialog}
          />
        }
        {(!this.state.isSendLinkShown) &&
          <Button onClick={this.toggleLinkDeleteDialog}>{gettext('Delete')}</Button>
        }
        {this.state.isLinkDeleteDialogOpen &&
          <CommonOperationConfirmationDialog
            title={gettext('Delete share link')}
            message={gettext('Are you sure you want to delete the share link?')}
            executeOperation={this.deleteLink}
            confirmBtnText={gettext('Delete')}
            toggleDialog={this.toggleLinkDeleteDialog}
          />
        }
      </div>
    );
  }
}

LinkDetails.propTypes = propTypes;

export default LinkDetails;
