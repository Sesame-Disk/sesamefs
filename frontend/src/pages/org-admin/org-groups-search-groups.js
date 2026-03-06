import React, { Component, Fragment } from 'react';
import PropTypes from 'prop-types';
import { Button, Dropdown, DropdownToggle, DropdownMenu, DropdownItem } from 'reactstrap';
import { Form, FormGroup, Input, Col } from 'reactstrap';
import { Utils } from '../../utils/utils';
import { seafileAPI } from '../../utils/seafile-api';
import { gettext, orgID, siteRoot } from '../../utils/constants';
import toaster from '../../components/toast';
import UserSelect from '../../components/user-select';
import OrgGroupInfo from '../../models/org-group';

class GroupItem extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      highlight: false,
      showMenu: false,
      isItemMenuShow: false
    };
  }

  onMouseEnter = () => {
    if (!this.props.isItemFreezed) {
      this.setState({
        showMenu: true,
        highlight: true,
      });
    }
  };

  onMouseLeave = () => {
    if (!this.props.isItemFreezed) {
      this.setState({
        showMenu: false,
        highlight: false
      });
    }
  };

  onDropdownToggleClick = (e) => {
    e.preventDefault();
    this.toggleOperationMenu(e);
  };

  toggleOperationMenu = (e) => {
    e.stopPropagation();
    this.setState(
      { isItemMenuShow: !this.state.isItemMenuShow }, () => {
        if (this.state.isItemMenuShow) {
          this.props.onFreezedItem();
        } else {
          this.setState({
            highlight: false,
            showMenu: false,
          });
          this.props.onUnfreezedItem();
        }
      }
    );
  };

  toggleDelete = () => {
    this.props.deleteGroupItem(this.props.group);
  };

  toggleTransfer = () => {
    this.props.openTransferDialog(this.props.group);
    this.props.onUnfreezedItem();
  };

  renderGroupHref = (group) => {
    let groupInfoHref;
    if (group.creatorName === 'system admin') {
      groupInfoHref = siteRoot + 'org/departmentadmin/groups/' + group.id + '/';
    } else {
      groupInfoHref = siteRoot + 'org/groupadmin/' + group.id + '/';
    }

    return groupInfoHref;
  };

  renderGroupCreator = (group) => {
    let userInfoHref = siteRoot + 'org/useradmin/info/' + group.creatorEmail + '/';
    if (group.creatorName === 'system admin') {
      return (
        <td> -- </td>
      );
    } else {
      return (
        <td>
          <a href={userInfoHref} className="font-weight-normal">{group.creatorName}</a>
        </td>
      );
    }
  };

  render() {
    let { group } = this.props;
    let isOperationMenuShow = (group.creatorName !== 'system admin') && this.state.showMenu;
    return (
      <tr className={this.state.highlight ? 'tr-highlight' : ''} onMouseEnter={this.onMouseEnter} onMouseLeave={this.onMouseLeave}>
        <td>
          <a href={this.renderGroupHref(group)} className="font-weight-normal">{group.groupName}</a>
        </td>
        {this.renderGroupCreator(group)}
        <td>{group.ctime}</td>
        <td className="text-center cursor-pointer">
          {isOperationMenuShow &&
            <Dropdown isOpen={this.state.isItemMenuShow} toggle={this.toggleOperationMenu}>
              <DropdownToggle
                tag="a"
                className="attr-action-icon fas fa-ellipsis-v"
                title={gettext('More operations')}
                aria-label={gettext('More operations')}
                data-toggle="dropdown"
                aria-expanded={this.state.isItemMenuShow}
                onClick={this.onDropdownToggleClick}
              />
              <DropdownMenu>
                <DropdownItem onClick={this.toggleTransfer}>{gettext('Transfer')}</DropdownItem>
                <DropdownItem onClick={this.toggleDelete}>{gettext('Delete')}</DropdownItem>
              </DropdownMenu>
            </Dropdown>
          }
        </td>
      </tr>
    );
  }
}

GroupItem.propTypes = {
  group: PropTypes.object.isRequired,
  isItemFreezed: PropTypes.bool.isRequired,
  onFreezedItem: PropTypes.func.isRequired,
  onUnfreezedItem: PropTypes.func.isRequired,
  deleteGroupItem: PropTypes.func.isRequired,
  openTransferDialog: PropTypes.func.isRequired,
};

class OrgGroupsSearchGroupsResult extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      isItemFreezed: false
    };
  }

  onFreezedItem = () => {
    this.setState({ isItemFreezed: true });
  };

  onUnfreezedItem = () => {
    this.setState({ isItemFreezed: false });
  };

  render() {
    let { orgGroups } = this.props;
    return (
      <div className="cur-view-content">
        <table>
          <thead>
            <tr>
              <th width="30%">{gettext('Name')}</th>
              <th width="35%">{gettext('Creator')}</th>
              <th width="23%">{gettext('Created At')}</th>
              <th width="12%" className="text-center">{gettext('Operations')}</th>
            </tr>
          </thead>
          <tbody>
            {orgGroups.map(item => {
              return (
                <GroupItem
                  key={item.id}
                  group={item}
                  isItemFreezed={this.state.isItemFreezed}
                  onFreezedItem={this.onFreezedItem}
                  onUnfreezedItem={this.onUnfreezedItem}
                  deleteGroupItem={this.props.toggleDelete}
                  openTransferDialog={this.props.openTransferDialog}
                />
              );
            })}
          </tbody>
        </table>
      </div>
    );
  }
}

OrgGroupsSearchGroupsResult.propTypes = {
  toggleDelete: PropTypes.func.isRequired,
  openTransferDialog: PropTypes.func.isRequired,
  orgGroups: PropTypes.array.isRequired,
};

class OrgGroupsSearchGroups extends Component {

  constructor(props) {
    super(props);
    this.state = {
      query: '',
      orgGroups: [],
      isSubmitBtnActive: false,
      loading: true,
      errorMsg: '',
      isTransferDialogOpen: false,
      currentGroup: null,
    };
  }

  componentDidMount() {
    let params = (new URL(document.location)).searchParams;
    this.setState({
      query: params.get('query') || '',
    }, () => { this.getItems(); });
  }

  getItems = () => {
    seafileAPI.orgAdminSearchGroup(orgID, this.state.query.trim()).then(res => {
      let groupList = res.data.group_list.map(item => {
        return new OrgGroupInfo(item);
      });
      this.setState({
        orgGroups: groupList,
        loading: false,
      });
    }).catch((error) => {
      this.setState({
        loading: false,
        errorMsg: Utils.getErrorMsg(error, true) // true: show login tip if 403
      });
    });
  };

  deleteGroupItem = (group) => {
    seafileAPI.orgAdminDeleteOrgGroup(orgID, group.id).then(res => {
      this.setState({
        orgGroups: this.state.orgGroups.filter(item => item.id !== group.id)
      });
      let msg = gettext('Successfully deleted {name}');
      msg = msg.replace('{name}', group.groupName);
      toaster.success(msg);
    }).catch(error => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  openTransferDialog = (group) => {
    this.setState({
      isTransferDialogOpen: true,
      currentGroup: group,
    });
  };

  closeTransferDialog = () => {
    this.setState({
      isTransferDialogOpen: false,
      currentGroup: null,
    });
  };

  transferGroupItem = (newOwnerEmail) => {
    const { currentGroup, orgGroups } = this.state;
    seafileAPI.orgAdminTransferGroup(orgID, currentGroup.id, newOwnerEmail).then(res => {
      const updatedGroups = orgGroups.map(item => {
        if (item.id === currentGroup.id) {
          return new OrgGroupInfo(res.data);
        }
        return item;
      });
      this.setState({ orgGroups: updatedGroups });
      let msg = gettext('Successfully transferred group {name}');
      msg = msg.replace('{name}', currentGroup.groupName);
      toaster.success(msg);
    }).catch(error => {
      let errMessage = Utils.getErrorMsg(error);
      toaster.danger(errMessage);
    });
  };

  handleInputChange = (e) => {
    this.setState({
      query: e.target.value
    }, this.checkSubmitBtnActive);
  };

  checkSubmitBtnActive = () => {
    const { query } = this.state;
    this.setState({
      isSubmitBtnActive: query.trim()
    });
  };

  handleKeyDown = (e) => {
    if (e.keyCode === 13) {
      const { isSubmitBtnActive } = this.state;
      if (isSubmitBtnActive) {
        this.getItems();
      }
    }
  };

  render() {
    const { query, isSubmitBtnActive, isTransferDialogOpen, currentGroup } = this.state;

    return (
      <Fragment>
        <div className="main-panel-center flex-row">
          <div className="cur-view-container">
            <div className="cur-view-path">
              <h3 className="sf-heading">{gettext('Groups')}</h3>
            </div>
            <div className="cur-view-content">
              <div className="mt-4 mb-6">
                <h4 className="border-bottom font-weight-normal mb-2 pb-1">{gettext('Search Groups')}</h4>
                <Form tag={'div'}>
                  <FormGroup row>
                    <Col sm={5}>
                      <Input type="text" name="query" value={query} placeholder={gettext('Search groups')} onChange={this.handleInputChange} onKeyDown={this.handleKeyDown} />
                    </Col>
                  </FormGroup>
                  <FormGroup row>
                    <Col sm={{ size: 5 }}>
                      <button className="btn btn-outline-primary" disabled={!isSubmitBtnActive} onClick={this.getItems}>{gettext('Submit')}</button>
                    </Col>
                  </FormGroup>
                </Form>
              </div>
              <div className="mt-4 mb-6">
                <h4 className="border-bottom font-weight-normal mb-2 pb-1">{gettext('Result')}</h4>
                <OrgGroupsSearchGroupsResult
                  toggleDelete={this.deleteGroupItem}
                  openTransferDialog={this.openTransferDialog}
                  orgGroups={this.state.orgGroups}
                />
              </div>
            </div>
          </div>
        </div>
        {isTransferDialogOpen && currentGroup && (
          <TransferGroupDialog
            groupName={currentGroup.groupName}
            transferGroup={this.transferGroupItem}
            toggleDialog={this.closeTransferDialog}
          />
        )}
      </Fragment>
    );
  }
}

class TransferGroupDialog extends React.Component {

  constructor(props) {
    super(props);
    this.state = {
      selectedOption: null,
      submitBtnDisabled: true,
    };
  }

  handleSelectChange = (option) => {
    this.setState({
      selectedOption: option,
      submitBtnDisabled: option === null,
    });
  };

  submit = () => {
    const receiver = this.state.selectedOption.email;
    this.props.transferGroup(receiver);
    this.props.toggleDialog();
  };

  render() {
    const { submitBtnDisabled } = this.state;
    const groupName = '<span class="op-target">' + Utils.HTMLescape(this.props.groupName) + '</span>';
    const msg = gettext('Transfer Group {placeholder} to').replace('{placeholder}', groupName);
    return (
      <div className="modal show d-block" tabIndex="-1" style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}>
        <div className="modal-dialog modal-dialog-centered">
          <div className="modal-content">
            <div className="modal-header">
              <h5 className="modal-title"><span dangerouslySetInnerHTML={{ __html: msg }}></span></h5>
              <button type="button" className="close" onClick={this.props.toggleDialog} aria-label="Close">
                <span aria-hidden="true">&times;</span>
              </button>
            </div>
            <div className="modal-body">
              <UserSelect
                ref="userSelect"
                isMulti={false}
                className="reviewer-select"
                placeholder={gettext('Select a user')}
                onSelectChange={this.handleSelectChange}
                searchFunc={(q) => seafileAPI.orgAdminSearchUser(orgID, q)}
              />
            </div>
            <div className="modal-footer">
              <Button color="secondary" onClick={this.props.toggleDialog}>{gettext('Cancel')}</Button>
              <Button color="primary" onClick={this.submit} disabled={submitBtnDisabled}>{gettext('Submit')}</Button>
            </div>
          </div>
        </div>
      </div>
    );
  }
}

TransferGroupDialog.propTypes = {
  groupName: PropTypes.string.isRequired,
  transferGroup: PropTypes.func.isRequired,
  toggleDialog: PropTypes.func.isRequired,
};

export default OrgGroupsSearchGroups;
